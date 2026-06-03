#!/bin/zsh
set -e

echo "Starting Automated Build Pipeline"

SCRIPT_DIR=${0:a:h}
CONFIG_FILE="$SCRIPT_DIR/config.env"

if [[ -f "$CONFIG_FILE" ]]; then
    source "$CONFIG_FILE"
else
    echo "Error: config.env not found in $SCRIPT_DIR"
    exit 1
fi

BUILD_ID="build-$(date +%s)"
WORK_DISK="work-${BUILD_ID}.img"
WORK_AUX="work-${BUILD_ID}-aux-img"

if [[ ! -f "$MASTER_DISK" ]] || [[ ! -f "$MASTER_AUX" ]]; then
    echo "Error: Master disk or aux not found"
    exit 1
fi

echo "Creating temp clones"
cp -c "$MASTER_DISK" "$WORK_DISK"
cp -c "$MASTER_AUX" "$WORK_AUX"

WORK_CONFIG="work-${BUILD_ID}.json"

# Reads vm.json and changes some variables (disk,aux,ram,cpu) and creates a temp verison to start the vm with
python3 -c "
import json
import sys

# Load the master config
with open('vm.json', 'r') as f:
    data = json.load(f)

for item in data.get('storage', []):
    if item['type'] == 'disk':
        item['file'] = '$WORK_DISK'
    elif item['type'] == 'aux':
        item['file'] ='$WORK_AUX'

data['ram'] = $VM_RAM_GB * 1024 * 1024 * 1024
data['cpus'] = $VM_CPU

with open('$WORK_CONFIG', 'w') as f:
    json.dump(data, f, indent=4)
"


IMAGE_URL="$HARBOR_REGISTRY/$PROJECT_NAME/$IMAGE_NAME:$IMAGE_TAG"

echo "Starting VM"
macosvm "$WORK_CONFIG" > vm_crash.log 2>&1 &
VM_PID=$!
echo "VM Process ID: $VM_PID"

MAX_RETRIES=60
COUNT=0


echo "Waiting for SSH connection"

while ! sshpass -p "$SSH_PASS" ssh -o ConnectTimeout=2 -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$SSH_USER@$VM_HOSTNAME" "echo 'SSH Ready'" 2>/dev/null; do
    printf "."
    sleep 2
    ((++COUNT)) 
    if [ $COUNT -ge $MAX_RETRIES ]; then
        echo "Timeout: Could not connect to $VM_HOSTNAME"
        kill $VM_PID
        rm "$WORK_DISK" "$WORK_AUX"
        exit 1
    fi
done

echo "SSH connection established"

BREW_LIST="${BREW_PACKAGES[*]}"
PIP_LIST="${PIP_PACKAGES[*]}"

echo "Installing software"
sshpass -p "$SSH_PASS" ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$SSH_USER@$VM_HOSTNAME" /bin/zsh <<REMOTE
    set -e

    export NONINTERACTIVE=1

    echo "Installing Homebrew"
    /bin/bash -c "\$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" < /dev/null > /dev/null

    echo 'eval "\$(/opt/homebrew/bin/brew shellenv)"' >> ~/.zprofile
    echo 'eval "\$(/opt/homebrew/bin/brew shellenv)"' >> ~/.zshenv
    eval "\$(/opt/homebrew/bin/brew shellenv)"

    echo "Installing Brew List"
    brew install $BREW_LIST < /dev/null

    echo "Installing PIP Packages"
    /opt/homebrew/bin/pip3 install --break-system-packages $PIP_LIST < /dev/null

    echo "Adding Homebrew to System PATH"
    sudo mkdir -p /etc/paths.d
    echo "/opt/homebrew/bin" | sudo tee /etc/paths.d/homebrew > /dev/null
    echo "/opt/homebrew/sbin" | sudo tee -a /etc/paths.d/homebrew > /dev/null

    echo "Dynamically creating symlinks for all Homebrew binaries"
    sudo mkdir -p /usr/local/bin
    sudo sh -c 'ln -sf /opt/homebrew/bin/* /usr/local/bin/'
    # Also explicitly provide the 'python' and 'pip' aliases (Homebrew uses 'python3' and 'pip3' by default)
    sudo ln -sf /opt/homebrew/bin/python3 /usr/local/bin/python
    sudo ln -sf /opt/homebrew/bin/pip3 /usr/local/bin/pip

    brew cleanup
    /opt/homebrew/bin/pip3 cache purge
    rm -rf ~/Library/Caches/*
REMOTE

echo "Stopping VM"
sshpass -p "$SSH_PASS" ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$SSH_USER@$VM_HOSTNAME" "sudo shutdown -h now" &

wait $VM_PID 2>/dev/null || true
echo "VM Stopped"


echo "Pushing to Harbor"
echo "$IMAGE_URL"
echo "$WORK_DISK"