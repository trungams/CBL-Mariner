// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package sliceutils

import (
	"os"
	"testing"

	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/logger"
	"github.com/microsoft/CBL-Mariner/toolkit/tools/internal/pkgjson"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	logger.InitStderrLog()
	os.Exit(m.Run())
}

func TestPackageVersSetToSliceShouldCreateEmptySliceFromNil(t *testing.T) {
	outputSlice := SetToSlice[*pkgjson.PackageVer](nil)

	assert.NotNil(t, outputSlice)
	assert.Empty(t, outputSlice)
}

func TestPackageVersSetToSliceShouldCreateEmptySliceFromEmptySet(t *testing.T) {
	outputSlice := SetToSlice(map[*pkgjson.PackageVer]bool{})

	assert.NotNil(t, outputSlice)
	assert.Empty(t, outputSlice)
}

func TestPackageVersSetToSliceShouldReturnValuesForAllTrueElementsInSet(t *testing.T) {
	existingPackageVer := &pkgjson.PackageVer{Name: "A"}
	missingPackageVer := &pkgjson.PackageVer{Name: "X"}
	inputSet := map[*pkgjson.PackageVer]bool{
		existingPackageVer: true,
		missingPackageVer:  false,
	}
	outputSlice := SetToSlice(inputSet)

	assert.NotNil(t, outputSlice)
	assert.Len(t, outputSlice, 1)
	assert.Contains(t, outputSlice, existingPackageVer)
	assert.NotContains(t, outputSlice, missingPackageVer)
}

func TestPackageVersShouldMatch(t *testing.T) {
	packageVer1 := &pkgjson.PackageVer{Name: "A"}
	packageVer2 := &pkgjson.PackageVer{Name: "A"}

	assert.True(t, PackageVerMatch(packageVer1, packageVer2))
}

func TestPackageVersShouldNotMatch(t *testing.T) {
	packageVer1 := &pkgjson.PackageVer{Name: "A"}
	packageVer2 := &pkgjson.PackageVer{Name: "B"}

	assert.False(t, PackageVerMatch(packageVer1, packageVer2))
}

func TestPackageVerShouldNotMatchNil(t *testing.T) {
	packageVer1 := &pkgjson.PackageVer{Name: "A"}

	assert.False(t, PackageVerMatch(packageVer1, nil))
}

func TestStringShouldMatch(t *testing.T) {
	assert.True(t, StringMatch("A", "A"))
}

func TestStringShouldNotMatch(t *testing.T) {
	assert.False(t, StringMatch("A", "B"))
}

func TestStringShouldNotMatchForNilFirst(t *testing.T) {
	assert.False(t, StringMatch(nil, "A"))
}

func TestStringShouldNotMatchNilSecond(t *testing.T) {
	assert.False(t, StringMatch("A", nil))
}

func TestStringShouldMatchForNilInBoth(t *testing.T) {
	assert.True(t, StringMatch(nil, nil))
}

func TestStringsSetToSliceShouldCreateEmptySliceFromNil(t *testing.T) {
	outputSlice := SetToSlice[string](nil)

	assert.NotNil(t, outputSlice)
	assert.Empty(t, outputSlice)
}

func TestStringsSetToSliceShouldCreateEmptySliceFromEmptySet(t *testing.T) {
	outputSlice := SetToSlice(map[string]bool{})

	assert.NotNil(t, outputSlice)
	assert.Empty(t, outputSlice)
}

func TestStringsSetToSliceShouldReturnValuesForAllTrueElementsInSet(t *testing.T) {
	inputSet := map[string]bool{
		"A": true,
		"B": true,
		"X": false,
		"Y": false,
	}
	outputSlice := SetToSlice(inputSet)

	assert.NotNil(t, outputSlice)
	assert.Len(t, outputSlice, 2)
	assert.Contains(t, outputSlice, "A")
	assert.Contains(t, outputSlice, "B")
	assert.NotContains(t, outputSlice, "X")
	assert.NotContains(t, outputSlice, "Y")
}
