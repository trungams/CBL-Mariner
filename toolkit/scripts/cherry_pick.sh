#!/bin/bash
# Copyright (c) Microsoft Corporation.
# Licensed under the MIT License.

set -e

function help {
    echo "hello, world"
}

function cherry_pick {
    commit_hash=$1
    target_branch=$2
    log_file=$3
    pr_title=$4
    $original_pr_url=$5
    tmp_branch="cherry-pick-$target_branch-$commit_hash"

    echo "Commit hash = $commit_hash"
    echo "Target branch = $target_branch"

    git checkout -b "$tmp_branch" origin/"$target_branch"

    git cherry-pick -x "$commit_hash" || rc=$?
    if [ ${rc:-0} -ne 0 ]; then
        echo "Cherry pick failed. Saving conflicts to log file"
        git diff --diff-filter=U > $log_file
        exit 1
    else
        git push -u origin "$tmp_branch"
        gh pr create \
            -B "$target_branch" \
            -H "$tmp_branch" \
            --title "[AUTO-CHERRY-PICK] $pr_title - branch $target_branch" \
            --body "This is an auto-generated pull request to cherry pick commit $commit_hash to $target_branch. Original PR: $original_pr_url" \
            > $log_file
    fi
}

commit_hash=
target_branch=
original_pr_url=
log_file=
pr_title=

while getopts "b:c:l:t:" opt; do
    case ${opt} in
    b ) target_branch="$OPTARG" ;;
    c ) commit_hash="$OPTARG" ;;
    l ) log_file="$OPTARG" ;;
    o ) original_pr_url="$OPTARG" ;;
    t ) pr_title="${OPTARG,,}" ;;
    ? ) echo -e "ERROR: Invalid option.\n\n"; help; exit 1 ;;
    esac
done

if [[ -z "$commit_hash" ]] || [[ -z "$target_branch" ]]; then
    echo -e "Error: arguments -c and -b are required"
    help
    exit 1
fi

cherry_pick $commit_hash $target_branch $log_file $pr_title
