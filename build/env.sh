#!/bin/sh

set -e

if [ ! -f "build/env.sh" ]; then
    echo "$0 must be run from the root of the repository."
    exit 2
fi

# Create fake Go workspace if it doesn't exist yet.
workspace="$PWD/build/workspace"
root="$PWD"
hpbdir="$workspace/src/github.com/shx-project"
if [ ! -L "$hpbdir/sphinx" ]; then
    mkdir -p "$hpbdir"
    cd "$hpbdir"
    ln -s ../../../../../. sphinx
    cd "$root"
fi

# Set up the environment to use the workspace.
GOPATH="$workspace"
export GOPATH

# Run the command inside the workspace.
cd "$hpbdir/sphinx"
PWD="$hpbdir/sphinx"

# Launch the arguments with the configured environment.
exec "$@"
