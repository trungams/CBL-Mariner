// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

// Utility to create and manipulate disks and partitions

package diskutils

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/microsoft/CBL-Mariner/toolkit/tools/imagegen/configuration"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/file"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/logger"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/retry"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/shell"
)

type blockDevicesOutput struct {
	Devices []blockDeviceInfo `json:"blockdevices"`
}

type blockDeviceInfo struct {
	Name   string      `json:"name"`    // Example: sda
	MajMin string      `json:"maj:min"` // Example: 1:2
	Size   json.Number `json:"size"`    // Number of bytes. Can be a quoted string or a JSON number, depending on the util-linux version
	Model  string      `json:"model"`   // Example: 'Virtual Disk'
}

// SystemBlockDevice defines a block device on the host computer
type SystemBlockDevice struct {
	DevicePath  string // Example: /dev/sda
	RawDiskSize uint64 // Size in bytes
	Model       string // Example: Virtual Disk
}

const (
	// AutoEndSize is used as the disk's "End" value to indicate it should be picked automatically
	AutoEndSize = 0
)

const (
	// mappingFilePath is used for device mapping paths
	mappingFilePath = "/dev/mapper/"

	// maxPrimaryPartitionsForMBR is the maximum number of primary partitions
	// allowed in the case of MBR partition
	maxPrimaryPartitionsForMBR = 4

	// name of all possible partition types
	primaryPartitionType  = "primary"
	extendedPartitionType = "extended"
	logicalPartitionType  = "logical"
)

// Unit to byte conversion values
// See https://www.gnu.org/software/parted/manual/parted.html#unit
const (
	B  = 1
	KB = 1000
	MB = 1000 * 1000
	GB = 1000 * 1000 * 1000
	TB = 1000 * 1000 * 1000 * 1000

	KiB = 1024
	MiB = 1024 * 1024
	GiB = 1024 * 1024 * 1024
	TiB = 1024 * 1024 * 1024 * 1024
)

var (
	sizeAndUnitRegexp = regexp.MustCompile(`(\d+)((Ki?|Mi?|Gi?|Ti?)?B)`)
	diskDevPathRegexp = regexp.MustCompile(`^/dev/(\w+)$`)

	unitToBytes = map[string]uint64{
		"B":   B,
		"KB":  KB,
		"MB":  MB,
		"GB":  GB,
		"TB":  TB,
		"KiB": KiB,
		"MiB": MiB,
		"GiB": GiB,
		"TiB": TiB,
	}
)

// BytesToSizeAndUnit takes a number of bytes and returns friendly representation of a size (for example 100GB).
func BytesToSizeAndUnit(bytes uint64) string {
	var (
		unitSize  uint64
		unitCount uint64
		unitName  string
	)

	sizes := []uint64{B, KiB, MiB, GiB, TiB}

	// Default to unit "Bytes" to handle the case where bytes is 0
	unitSize = B

	for _, unit := range sizes {
		if bytes >= unit {
			unitSize = unit
		}
	}

	for unit, unitBytes := range unitToBytes {
		if unitBytes == unitSize {
			unitName = unit
			break
		}
	}

	unitCount = bytes / unitSize

	return fmt.Sprintf("%d%s", unitCount, unitName)
}

// SizeAndUnitToBytes takes a friendly representation of a size (for example 100GB) and return the number of bytes it represents.
func SizeAndUnitToBytes(sizeAndUnit string) (bytes uint64, err error) {
	const (
		sizeIndex = 1
		unitIndex = 2
	)

	// Match size and unit.  Examples: 2GB, 512MiB
	matches := sizeAndUnitRegexp.FindAllStringSubmatch(sizeAndUnit, -1)

	// must be at least one match
	if len(matches) == 0 || len(matches[0]) <= 2 {
		err = fmt.Errorf("sizeAndUnit must contain a number and a unit type")
		return
	}
	match := matches[0]

	sizeString := match[sizeIndex]
	unit := match[unitIndex]

	size, err := strconv.ParseUint(sizeString, 10, 64)
	if err != nil {
		return
	}

	if unitBytes, ok := unitToBytes[unit]; ok {
		bytes = size * unitBytes
	} else {
		err = fmt.Errorf("unknown unit type (%s)", unit)
		return
	}

	return
}

// ApplyRawBinaries applies all raw binaries described in disk configuration to the specified disk
func ApplyRawBinaries(diskDevPath string, disk configuration.Disk) (err error) {
	rawBinaries := disk.RawBinaries

	for idx := range rawBinaries {
		rawBinary := rawBinaries[idx]
		err = ApplyRawBinary(diskDevPath, rawBinary)
		if err != nil {
			return err
		}
	}
	return
}

// ApplyRawBinary applies a single raw binary at offset (seek) with blocksize to the specified disk
func ApplyRawBinary(diskDevPath string, rawBinary configuration.RawBinary) (err error) {
	ddArgs := []string{
		fmt.Sprintf("if=%s", rawBinary.BinPath),   // Input file.
		fmt.Sprintf("of=%s", diskDevPath),         // Output file.
		fmt.Sprintf("bs=%d", rawBinary.BlockSize), // Size of one copied block.
		fmt.Sprintf("seek=%d", rawBinary.Seek),    // Block number to start copying in the output file at.
		"conv=notrunc",                            // Prevent truncation.
	}

	_, stderr, err := shell.Execute("dd", ddArgs...)
	if err != nil {
		logger.Log.Warnf("Failed to apply raw binary with dd: %v", stderr)
	}
	return
}

// CreateEmptyDisk creates an empty raw disk in the given working directory as described in disk configuration
func CreateEmptyDisk(workDirPath, diskName string, disk configuration.Disk) (diskFilePath string, err error) {
	const (
		defautBlockSize = MiB
	)
	diskFilePath = filepath.Join(workDirPath, diskName)

	// Assume that Disk.MaxSize is given
	maxSize := disk.MaxSize
	err = sparseDisk(diskFilePath, defautBlockSize, maxSize)
	return
}

// sparseDisk creates an empty sparse disk file.
func sparseDisk(diskPath string, blockSize, size uint64) (err error) {
	ddArgs := []string{
		"if=/dev/zero",                  // Input file.
		fmt.Sprintf("of=%s", diskPath),  // Output file.
		fmt.Sprintf("bs=%d", blockSize), // Size of one copied block.
		fmt.Sprintf("seek=%d", size),    // Size of the image.
		"count=0",                       // Number of blocks to copy to the output file.
	}

	_, stderr, err := shell.Execute("dd", ddArgs...)
	if err != nil {
		logger.Log.Warnf("Failed to create empty disk with dd: %v", stderr)
	}
	return
}

// SetupLoopbackDevice creates a /dev/loop device for the given disk file
func SetupLoopbackDevice(diskFilePath string) (devicePath string, err error) {
	stdout, stderr, err := shell.Execute("losetup", "--show", "-f", "-P", diskFilePath)
	if err != nil {
		logger.Log.Warnf("Failed to create loopback device using losetup: %v", stderr)
		return
	}
	devicePath = strings.TrimSpace(stdout)
	logger.Log.Debugf("Created loopback device at device path: %v", devicePath)
	return
}

// BlockOnDiskIO waits until all outstanding operations against a disk complete.
func BlockOnDiskIO(diskDevPath string) (err error) {
	const (
		// Indices for values in /proc/diskstats
		majIdx            = 0
		minIdx            = 1
		outstandingOpsIdx = 11
	)
	var blockDevices blockDevicesOutput

	logger.Log.Infof("Flushing all IO to disk for %s", diskDevPath)
	_, _, err = shell.Execute("sync")
	if err != nil {
		return
	}

	rawDiskOutput, stderr, err := shell.Execute("lsblk", "--nodeps", "--json", "--output", "NAME,MAJ:MIN", diskDevPath)
	if err != nil {
		logger.Log.Warn(stderr)
		return
	}

	bytes := []byte(rawDiskOutput)
	err = json.Unmarshal(bytes, &blockDevices)
	if err != nil {
		return
	}

	if len(blockDevices.Devices) != 1 {
		return fmt.Errorf("couldn't find disk IDs for %s (%s), expecting only one result", diskDevPath, rawDiskOutput)
	}
	// MAJ:MIN is returned in the form "1:2"
	diskIDs := strings.Split(blockDevices.Devices[0].MajMin, ":")
	if len(diskIDs) != 2 {
		return fmt.Errorf("couldn't find disk IDs for %s (%s), couldn't parse MAJ:MIN", diskDevPath, rawDiskOutput)
	}
	maj := diskIDs[0]
	min := diskIDs[1]

	logger.Log.Tracef("Searching /proc/diskstats for %s (%s:%s)", blockDevices.Devices[0].Name, maj, min)
	for {
		var (
			foundEntry     = false
			outstandingOps = ""
		)

		// Find the entry with Major#, Minor#, ..., IOs which matches our disk
		onStdout := func(args ...interface{}) {

			// Bail early if we already found the entry
			if foundEntry {
				return
			}

			line := args[0].(string)
			deviceStatsFields := strings.Fields(line)
			if maj == deviceStatsFields[majIdx] && min == deviceStatsFields[minIdx] {
				outstandingOps = deviceStatsFields[outstandingOpsIdx]
				foundEntry = true
			}
		}

		err = shell.ExecuteLiveWithCallback(onStdout, logger.Log.Error, true, "cat", "/proc/diskstats")
		if err != nil {
			logger.Log.Error(stderr)
			return
		}
		if !foundEntry {
			return fmt.Errorf("couldn't find entry for '%s' in /proc/diskstats", diskDevPath)
		}
		logger.Log.Debugf("Outstanding operations on '%s': %s", diskDevPath, outstandingOps)

		if outstandingOps == "0" {
			break
		}

		// Sleep breifly
		time.Sleep(time.Second / 4)
	}
	return
}

// DetachLoopbackDevice detaches the specified disk
func DetachLoopbackDevice(diskDevPath string) (err error) {
	logger.Log.Infof("Detaching Loopback Device Path: %v", diskDevPath)
	_, stderr, err := shell.Execute("losetup", "-d", diskDevPath)
	if err != nil {
		logger.Log.Warnf("Failed to detach loopback device using losetup: %v", stderr)
	}
	return
}

// CreatePartitions creates partitions on the specified disk according to the disk config
func CreatePartitions(diskDevPath string, disk configuration.Disk, rootEncryption configuration.RootEncryption, readOnlyRootConfig configuration.ReadOnlyVerityRoot) (partDevPathMap map[string]string, partIDToFsTypeMap map[string]string, encryptedRoot EncryptedRootDevice, readOnlyRoot VerityDevice, err error) {
	const timeoutInSeconds = "5"
	partDevPathMap = make(map[string]string)
	partIDToFsTypeMap = make(map[string]string)

	// Clear any old partition table info to prevent errors during partition creation
	_, stderr, err := shell.Execute("sfdisk", "--delete", diskDevPath)
	if err != nil {
		logger.Log.Warnf("Failed to clear partition table. Expected if the disk is blank: %v", stderr)
	}

	// Create new partition table
	partitionTableType := disk.PartitionTableType
	logger.Log.Debugf("Converting partition table type (%v) to parted argument", partitionTableType)
	partedArgument, err := partitionTableType.ConvertToPartedArgument()
	if err != nil {
		logger.Log.Errorf("Unable to convert partition table type (%v) to parted argument", partitionTableType)
		return
	}
	_, stderr, err = shell.Execute("flock", "--timeout", timeoutInSeconds, diskDevPath, "parted", diskDevPath, "--script", "mklabel", partedArgument)
	if err != nil {
		logger.Log.Warnf("Failed to set partition table type using parted: %v", stderr)
		return
	}

	usingExtendedPartition := (len(disk.Partitions) > maxPrimaryPartitionsForMBR) && (partitionTableType == configuration.PartitionTableTypeMbr)

	// Partitions assumed to be defined in sorted order
	for idx, partition := range disk.Partitions {
		partType, partitionNumber := obtainPartitionDetail(idx, usingExtendedPartition)
		// Insert an extended partition
		if partType == extendedPartitionType {
			err = createExtendedPartition(diskDevPath, partitionTableType.String(), disk.Partitions, partIDToFsTypeMap, partDevPathMap)
			if err != nil {
				return
			}

			// Update partType and partitionNumber
			partType = logicalPartitionType
			partitionNumber = partitionNumber + 1
		}

		partDevPath, err := CreateSinglePartition(diskDevPath, partitionNumber, partitionTableType.String(), partition, partType)
		if err != nil {
			logger.Log.Warnf("Failed to create single partition")
			return partDevPathMap, partIDToFsTypeMap, encryptedRoot, readOnlyRoot, err
		}

		partFsType, err := FormatSinglePartition(partDevPath, partition)
		if err != nil {
			logger.Log.Warnf("Failed to format partition")
			return partDevPathMap, partIDToFsTypeMap, encryptedRoot, readOnlyRoot, err
		}

		if rootEncryption.Enable && partition.HasFlag(configuration.PartitionFlagDeviceMapperRoot) {
			encryptedRoot, err = encryptRootPartition(partDevPath, partition, rootEncryption)
			if err != nil {
				logger.Log.Warnf("Failed to initialize encrypted root")
				return partDevPathMap, partIDToFsTypeMap, encryptedRoot, readOnlyRoot, err
			}
			partDevPathMap[partition.ID] = GetEncryptedRootVolMapping()
		} else if readOnlyRootConfig.Enable && partition.HasFlag(configuration.PartitionFlagDeviceMapperRoot) {
			readOnlyRoot, err = PrepReadOnlyDevice(partDevPath, partition, readOnlyRootConfig)
			if err != nil {
				logger.Log.Warnf("Failed to initialize read only root")
				return partDevPathMap, partIDToFsTypeMap, encryptedRoot, readOnlyRoot, err
			}
			partDevPathMap[partition.ID] = readOnlyRoot.MappedDevice
		} else {
			partDevPathMap[partition.ID] = partDevPath
		}

		partIDToFsTypeMap[partition.ID] = partFsType
	}
	return
}

// CreateSinglePartition creates a single partition based on the partition config
func CreateSinglePartition(diskDevPath string, partitionNumber int, partitionTableType string, partition configuration.Partition, partType string) (partDevPath string, err error) {
	const (
		fillToEndOption  = "100%"
		sFmt             = "%ds"
		timeoutInSeconds = "5"
	)

	logicalSectorSize, physicalSectorSize, err := getSectorSize(diskDevPath)
	if err != nil {
		return
	}

	start := partition.Start * MiB / logicalSectorSize
	end := partition.End*MiB/logicalSectorSize - 1
	if partition.End == 0 {
		end = 0
	}

	if partType == logicalPartitionType {
		start = start + 1
		if end != 0 {
			end = end + 1
		}
	}

	// Check wehther the start sector is 4K-aligned
	start = alignSectorAddress(start, logicalSectorSize, physicalSectorSize)

	logger.Log.Debugf("Input partition start: %d, aligned start sector: %d", partition.Start, start)
	logger.Log.Debugf("Input partition end: %d, end sector: %d", partition.End, end)

	fsType := partition.FsType

	if end == 0 {
		_, stderr, err := shell.Execute("flock", "--timeout", timeoutInSeconds, diskDevPath, "parted", diskDevPath, "--script", "mkpart", partType, fsType, fmt.Sprintf(sFmt, start), fillToEndOption)
		if err != nil {
			logger.Log.Warnf("Failed to create partition using parted: %v", stderr)
			return "", err
		}
	} else {
		_, stderr, err := shell.Execute("flock", "--timeout", timeoutInSeconds, diskDevPath, "parted", diskDevPath, "--script", "mkpart", partType, fsType, fmt.Sprintf(sFmt, start), fmt.Sprintf(sFmt, end))
		if err != nil {
			logger.Log.Warnf("Failed to create partition using parted: %v", stderr)
			return "", err
		}
	}
	// Update kernel partition table information
	//
	// There can be a timing issue where partition creation finishes but the
	// devtmpfs files are not populated in time for partition initialization.
	// So to deal with this, we call partprobe here to query and flush the
	// partition table information, which should enforce that the devtmpfs
	// files are created when partprobe returns control.
	//
	// Added flock because "partprobe -s" apparently doesn't always block.
	// flock is part of the util-linux package and helps to synchronize access
	// with other cooperating processes. The important part is it will block
	// if the fd is busy, and then execute the command. Adding a timeout
	// to prevent us from possibly waiting forever.
	stdout, stderr, err := shell.Execute("flock", "--timeout", timeoutInSeconds, diskDevPath, "partprobe", "-s", diskDevPath)
	if err != nil {
		logger.Log.Warnf("Failed to execute partprobe: %v", stderr)
		return "", err
	}
	logger.Log.Debugf("Partprobe -s returned: %s", stdout)
	return InitializeSinglePartition(diskDevPath, partitionNumber, partitionTableType, partition)
}

// InitializeSinglePartition initializes a single partition based on the given partition configuration
func InitializeSinglePartition(diskDevPath string, partitionNumber int, partitionTableType string, partition configuration.Partition) (partDevPath string, err error) {
	const (
		retryDuration    = time.Second
		timeoutInSeconds = "5"
		totalAttempts    = 5
	)

	partitionNumberStr := strconv.Itoa(partitionNumber)

	// There are two primary partition naming conventions:
	// /dev/sdN<y> style or /dev/loopNp<x> style
	// Detect the exact one we are using.
	// Make sure we check for /dev/loopNp<x> FIRST, since /dev/loop1 would generate /dev/loop11 as a partition
	// device which may be a valid device. We want to select /dev/loop1p1 first.
	testPartDevPaths := []string{
		fmt.Sprintf("%sp%s", diskDevPath, partitionNumberStr),
		fmt.Sprintf("%s%s", diskDevPath, partitionNumberStr),
	}

	err = retry.Run(func() error {
		for _, testPartDevPath := range testPartDevPaths {
			exists, err := file.PathExists(testPartDevPath)
			if err != nil {
				logger.Log.Errorf("Error finding device path (%s)", testPartDevPath)
				return err
			}
			if exists {
				partDevPath = testPartDevPath
				return nil
			}
			logger.Log.Debugf("Could not find partition path (%s). Checking other naming convention", testPartDevPath)
		}
		logger.Log.Warnf("Could not find any valid partition paths. Will retry up to %d times", totalAttempts)
		err = fmt.Errorf("could not find partition to initialize in /dev")
		return err
	}, totalAttempts, retryDuration)

	if err != nil {
		logger.Log.Errorf("%s", err)
		return
	}

	logger.Log.Debugf("Initializing partition device path: %v", partDevPath)

	// Set partition friendly name (only for gpt)
	if partitionTableType == "gpt" {
		partitionName := partition.Name
		_, stderr, err := shell.Execute("flock", "--timeout", timeoutInSeconds, diskDevPath, "parted", diskDevPath, "--script", "name", partitionNumberStr, partitionName)
		if err != nil {
			logger.Log.Warnf("Failed to set partition friendly name using parted: %v", stderr)
			// Not-fatal
		}
	}

	// Set partition flags if necessary
	for _, flag := range partition.Flags {
		args := []string{diskDevPath, "--script", "set", partitionNumberStr}
		var flagToSet string
		switch flag {
		case configuration.PartitionFlagESP:
			flagToSet = "esp"
		case configuration.PartitionFlagGrub, configuration.PartitionFlagBiosGrub, configuration.PartitionFlagBiosGrubLegacy:
			flagToSet = "bios_grub"
		case configuration.PartitionFlagBoot:
			flagToSet = "boot"
		case configuration.PartitionFlagDeviceMapperRoot:
			//Ignore, only used for internal tooling
		default:
			return partDevPath, fmt.Errorf("partition %v - Unknown partition flag: %v", partitionNumber, flag)
		}
		if flagToSet != "" {
			args = append(args, flagToSet, "on")
			// Golang does not allow mixing of variadic and regular arguments. So add all of the flock args to
			// the overall arg slice and pass that to execute
			args = append([]string{"--timeout", timeoutInSeconds, diskDevPath, "parted"}, args...)
			_, stderr, err := shell.Execute("flock", args...)
			if err != nil {
				logger.Log.Warnf("Failed to set flag (%s) using parted: %v", flagToSet, stderr)
			}
		}
	}

	// Make sure all partition information is actually updated.
	stdout, stderr, err := shell.Execute("flock", "--timeout", timeoutInSeconds, diskDevPath, "partprobe", "-s", diskDevPath)
	if err != nil {
		logger.Log.Warnf("Failed to execute partprobe after partition initialization: %v", stderr)
		return "", err
	}
	logger.Log.Debugf("Partprobe -s returned: %s", stdout)

	return
}

// FormatSinglePartition formats the given partition to the type specified in the partition configuration
func FormatSinglePartition(partDevPath string, partition configuration.Partition) (fsType string, err error) {
	const (
		totalAttempts = 5
		retryDuration = time.Second
	)

	fsType = partition.FsType

	// Note: It is possible for the format partition command to fail with error "The file does not exist and no size was specified".
	// This is due to a possible race condition in Linux/parted where the partition may not actually be ready after being newly created.
	// To handle such cases, we can retry the command.
	switch fsType {
	case "fat32", "fat16", "vfat", "ext2", "ext3", "ext4", "xfs":
		if fsType == "fat32" || fsType == "fat16" {
			fsType = "vfat"
		}
		err = retry.Run(func() error {
			_, stderr, err := shell.Execute("mkfs", "-t", fsType, partDevPath)
			if err != nil {
				logger.Log.Warnf("Failed to format partition using mkfs: %v", stderr)
				return err
			}

			return err
		}, totalAttempts, retryDuration)
		if err != nil {
			err = fmt.Errorf("could not format partition with type %v after %v retries", fsType, totalAttempts)
		}
	case "linux-swap":
		err = retry.Run(func() error {
			_, stderr, err := shell.Execute("mkswap", partDevPath)
			if err != nil {
				logger.Log.Warnf("Failed to format swap partition using mkswap: %v", stderr)
				return err
			}
			return err
		}, totalAttempts, retryDuration)
		if err != nil {
			err = fmt.Errorf("could not format partition with type %v after %v retries", fsType, totalAttempts)
		}

		_, stderr, err := shell.Execute("swapon", partDevPath)
		if err != nil {
			logger.Log.Warnf("Failed to execute swapon: %v", stderr)
			return "", err
		}
	case "":
		logger.Log.Debugf("No filesystem type specified. Ignoring for partition: %v", partDevPath)
	default:
		return fsType, fmt.Errorf("unrecognized filesystem format: %v", fsType)
	}

	return
}

// SystemBlockDevices returns all block devices on the host system.
func SystemBlockDevices() (systemDevices []SystemBlockDevice, err error) {
	const (
		scsiDiskMajorNumber      = "8"
		mmcBlockMajorNumber      = "179"
		virtualDiskMajorNumber   = "252,253,254"
		blockExtendedMajorNumber = "259"
	)
	var blockDevices blockDevicesOutput

	blockDeviceMajorNumbers := []string{scsiDiskMajorNumber, mmcBlockMajorNumber, virtualDiskMajorNumber, blockExtendedMajorNumber}
	includeFilter := strings.Join(blockDeviceMajorNumbers, ",")
	rawDiskOutput, stderr, err := shell.Execute("lsblk", "-d", "--bytes", "-I", includeFilter, "-n", "--json", "--output", "NAME,SIZE,MODEL")
	if err != nil {
		logger.Log.Warn(stderr)
		return
	}
	if len(rawDiskOutput) == 0 {
		err = fmt.Errorf("no supported disks found")
		logger.Log.Errorf("%s", err)
		return
	}

	bytes := []byte(rawDiskOutput)
	err = json.Unmarshal(bytes, &blockDevices)
	if err != nil {
		return
	}

	systemDevices = make([]SystemBlockDevice, len(blockDevices.Devices))

	for i, disk := range blockDevices.Devices {
		systemDevices[i].DevicePath = fmt.Sprintf("/dev/%s", disk.Name)

		systemDevices[i].RawDiskSize, err = strconv.ParseUint(disk.Size.String(), 10, 64)
		if err != nil {
			return
		}

		systemDevices[i].Model = strings.TrimSpace(disk.Model)
	}

	return
}

func createExtendedPartition(diskDevPath string, partitionTableType string, partitions []configuration.Partition, partIDToFsTypeMap, partDevPathMap map[string]string) (err error) {
	// Create a new partition object for extended partition
	extendedPartition := configuration.Partition{}
	extendedPartition.ID = extendedPartitionType
	extendedPartition.Start = partitions[maxPrimaryPartitionsForMBR-1].Start
	extendedPartition.End = partitions[len(partitions)-1].End

	partDevPath, err := CreateSinglePartition(diskDevPath, maxPrimaryPartitionsForMBR, partitionTableType, extendedPartition, extendedPartitionType)
	if err != nil {
		logger.Log.Warnf("Failed to create extended partition")
		return
	}
	partIDToFsTypeMap[extendedPartition.ID] = ""
	partDevPathMap[extendedPartition.ID] = partDevPath
	return
}

func getPartUUID(device string) (uuid string, err error) {
	stdout, _, err := shell.Execute("blkid", device, "-s", "UUID", "-o", "value")
	if err != nil {
		return
	}
	logger.Log.Trace(stdout)
	uuid = strings.TrimSpace(stdout)
	return
}

func getSectorSizeFromFile(sectorFile string) (sectorSize uint64, err error) {
	if exists, ferr := file.PathExists(sectorFile); ferr != nil {
		logger.Log.Errorf("Error accessing sector size file %s", sectorFile)
		err = ferr
		return
	} else if !exists {
		err = fmt.Errorf("could not find the hw sector size file %s to obtain the sector size of the system", sectorFile)
		return
	}

	fileContent, err := file.ReadLines(sectorFile)
	if err != nil {
		logger.Log.Errorf("Failed to read from %s: %s", sectorFile, err)
		return
	}

	// sector file should only have one line, return error if not
	if len(fileContent) != 1 {
		err = fmt.Errorf("%s has more than one line", sectorFile)
		return
	}

	sectorSize, err = strconv.ParseUint(fileContent[0], 10, 64)
	return
}

func getSectorSize(diskDevPath string) (logicalSectorSize, physicalSectorSize uint64, err error) {
	const (
		diskNameStartIndex = 5
	)

	// Grab the specific disk name from /dev/xxx
	matchResult := diskDevPathRegexp.MatchString(diskDevPath)
	if !matchResult {
		err = fmt.Errorf("input disk device path (%s) is of invalud format", diskDevPath)
		return
	}
	diskName := diskDevPath[diskNameStartIndex:len(diskDevPath)]

	hw_sector_size_file := fmt.Sprintf("/sys/block/%s/queue/hw_sector_size", diskName)
	physical_sector_size_file := fmt.Sprintf("/sys/block/%s/queue/physical_block_size", diskName)

	logicalSectorSize, err = getSectorSizeFromFile(hw_sector_size_file)
	if err != nil {
		return
	}

	physicalSectorSize, err = getSectorSizeFromFile(physical_sector_size_file)
	return
}

func alignSectorAddress(sectorAddr, logicalSectorSize, physicalSectorSize uint64) (alignedSector uint64) {
	// Need to make sure that starting sector of a partition is aligned based on the physical sector size of the system.
	// For example, suppose the physical sector size is 4096. If the input start sector is 40960001, then this is misaligned,
	// and need to be elevated to the next aligned address, which is (40960001/4096 + 1)*4096 = 4100096.

	// We do need to take care of a special case, which is the first partition (normally boot partition) might be less than
	// the physical sector size. In this case, we need to check whether the start sector is a multiple of 1 MiB.
	alignedSector = 0
	if sectorAddr < physicalSectorSize {
		if sectorAddr%(MiB/logicalSectorSize) == 0 {
			alignedSector = sectorAddr
		}
	} else if (sectorAddr % physicalSectorSize) == 0 {
		alignedSector = sectorAddr
	} else {
		alignedSector = (sectorAddr/physicalSectorSize + 1) * physicalSectorSize
	}

	return
}

func obtainPartitionDetail(partitionIndex int, hasExtendedPartition bool) (partType string, partitionNumber int) {
	const (
		indexOffsetForNormalPartitionNumber  = 1
		indexOffsetForLogicalPartitionNumber = 2
	)

	// partitionIndex is the index of the partition in the partition array, which starts at 0.
	// partitionNumber, however, starts at 1 (E.g. /dev/sda1), and thus partitionNumber = partitionIndex + 1.
	// In the case of logical partitions, since an extra extended partition has to be created first in order to
	// to create logical partitions, so the partition number will further increase by 1, which equals partitionIndex + 2.

	if hasExtendedPartition && partitionIndex >= (maxPrimaryPartitionsForMBR-1) {
		if partitionIndex == (maxPrimaryPartitionsForMBR - 1) {
			partType = extendedPartitionType
			partitionNumber = partitionIndex + indexOffsetForNormalPartitionNumber
		} else {
			partType = logicalPartitionType
			partitionNumber = partitionIndex + indexOffsetForLogicalPartitionNumber
		}
	} else {
		partType = primaryPartitionType
		partitionNumber = partitionIndex + indexOffsetForNormalPartitionNumber
	}

	return
}
