# Developer Notes & Setup (macOS-vz-kubelet)

These notes describe the setup, build process, and troubleshooting for the project based on previous experiences and configurations.

## Image Building Workflow

To create and distribute custom macOS VM images for the kubelet, follow this three-step pipeline:

### 1. Setup the Base Image (Macosvm)
First, download a macOS IPSW firmware image from Apple. Fetch the latest URL by running:
```bash
curl -s https://mesu.apple.com/assets/macos/com_apple_macOSIPSW/com_apple_macOSIPSW.xml | grep -o 'https://updates.cdn-apple.com/[^<]*\.ipsw'
```
*(For older or specific macOS versions, find the IPSW files at the [Mr. Macintosh Firmware Database](https://mrmacintosh.com/apple-silicon-m1-full-macos-restore-ipsw-firmware-files-database/)).*

Once downloaded, create a base "master" macOS VM image using `macosvm`. 
* This initial setup requires some manual GUI interaction for going through the initial macOS setup procedures.
* Once booted, manually configure the network inside the VM to get internet access. Run this inside the VM:
  ```bash
  networksetup -setmanual "Ethernet" <IP>
  ```
  * Set the `<IP>` to the value from the host machine's `bridge100` interface, found by running `ifconfig -a` on the host.

### 2. Customize the Image (`buildimage/build.sh`)
Once the master image is done, use the automated build script found in the `buildimage/` folder.
This script takes a copy of your "master" image and installs the required packages and libraries via `brew` and `pip install`. The `config.env` file contains the list of packages to install as well as other required settings, while the `vm.json` file provides the hardware configuration (like CPU, RAM, and base model IDs) for the temporary VM used during the build process. *(Note: The CPU and RAM defined here are strictly for the build stage. When the final image is actually deployed, the CPU and RAM limits defined in the Kubernetes Job/Pod manifest will be used instead).* This makes it easy to declaratively create the exact image you need (similar to how a `Dockerfile` works).
*Note: The script must be run with `sudo`.*

### 3. Push the Image to a Registry (`oras-macos-vz`)
After the build script finishes, push your newly built image to an OCI registry using `oras-macos-vz` (a fork of `oras` custom-built to work with `macOS-vz-kubelet` artifacts).
* Log in to your registry like you would with Docker (`oras-macos-vz login ...`).
* Push the artifact. Here is an example command:

```bash
oras-macos-vz push myregistry.example.com/macos-images/macos-thesis:v1 \
    --disk-path /path/to/work-build-1780477302.img \
    --aux-path /path/to/work-build-1780477302-aux-img \
    --hardware-model "YnBsaXN0MDDTAQIDBAQFXxAZRGF0YVJlcHJlc2VudGF0aW9uVmVyc2lvbl8QD1BsYXRmb3JtVmVyc2lvbl8QEk1pbmltdW1TdXBwb3J0ZWRPUxACowYHBxANEAAIDys9UlRYWgAAAAAAAAEBAAAAAAAAAAgAAAAAAAAAAAAAAAAAAABc" \
    --machine-id "YnBsaXN0MDDRAQJURUNJRBQAAAAAAAAAAOVBaP/pWdByCAsQAAAAAAAAAQEAAAAAAAAAAwAAAAAAAAAAAAAAAAAAACE=" \
    --verbose
```
*(Note: The `--hardware-model` and `--machine-id` base64 strings above are specific to the Apple Silicon generation used during setup (M4 chip in this case). You may need to change these strings to match newer hardware if the host machine architecture changes).*

## Connecting to Kubernetes
`admin.conf` was copied from one of the control-planes (with minor changes, such as the IP the server points to - either to a loadbalancer or directly to a control-plane). 
To make this work with the virtual-kubelet, the certificate data inside `admin.conf` was split out into separate files and mapped to environment variables:
```bash
export APISERVER_CA_CERT_LOCATION="$HOME/.kube/certs/ca.crt"
export APISERVER_CERT_LOCATION="$HOME/.kube/certs/tls.crt"
export APISERVER_KEY_LOCATION="$HOME/.kube/certs/tls.key"
export KUBECONFIG="$HOME/.kube/config"
```

*Security Note & Future Improvement:* Using `admin.conf` directly on the worker node is a significant security risk, as it grants full cluster admin privileges to the kubelet. Ideally, you should generate a dedicated kubelet client certificate, have it signed by the Kubernetes Certificate Authority (CA), and assign it narrowly scoped RBAC permissions instead.

## Plist & Startup
The Plist file located in `macOS-vz-kubelet` was rewritten to point to the correct names/labels and the correct certificates under `.kube/certs/` and `.kube/config`.
Because the connection is made via SSH, standard user `launchctl` commands don't work directly. This can be bypassed using `sudo` to configure it as a system-level LaunchDaemon. Setting it up as a LaunchDaemon (though untested in this implementation) is the recommended approach, as it ensures the virtual-kubelet node will automatically restart if the host machine shuts down or reboots unexpectedly. However, for immediate simplicity, the application is just run using `nohup`.

## Before installing macOS-vz-kubelet
Run the following in the root folder of the project to build and sign the binary:
```bash
# Build the binary
go build -o ../builds/macos-vz-kubelet ./cmd/virtual-kubelet/

# Sign the binary with entitlements
codesign --entitlements resources/vz.entitlements -s - ../builds/macos-vz-kubelet
```

Start the kubelet with the following environment variables:
```bash
export APISERVER_CA_CERT_LOCATION="$HOME/.kube/certs/ca.crt"
export APISERVER_CERT_LOCATION="$HOME/.kube/certs/tls.crt"
export APISERVER_KEY_LOCATION="$HOME/.kube/certs/tls.key"
export DOCKER_HOST="$HOME/.colima/docker.sock"
export KUBECONFIG="$HOME/.kube/config"
export VZ_SSH_PASSWORD="thesis"  # Note: This user/password must exactly match the ones defined in your config.env!
export VZ_SSH_USER="thesis"

nohup /Users/ubuntu/.thesis/builds/macos-vz-kubelet \
  --authentication-token-webhook true \
  --nodename mac-mini-vm \
  --log-level info \
  > /Users/ubuntu/Library/Logs/com.thesis.virtualization/output.log 2>&1 &
```

## Requirements
* Certain tools come pre-installed (e.g., `make`, Xcode command-line tools for `codesign`).
* **Go 1.25.2:** Was installed manually due to version requirements.
* **Docker/Colima:** Installed via `brew`. Docker is needed to run sidecars (e.g., for logging). Colima is used (MIT license) to avoid needing Docker Desktop. *(Note: While not explicitly tested in this specific implementation, Docker sidecar functionality is fully inherited from the upstream `macOS-vz-kubelet` project and should work as designed).*

## Issues with VM Virtualization.Framework
Read more about the issue and workarounds at [tart.run/faq](https://tart.run/faq/).

As of macOS 15 (which the Mac mini runs / second newest, 26 also exists), there is an undocumented requirement that an unlocked `login.keychain` must be available when running a VM.
This can be resolved by logging in via the GUI and configuring *automatic log in*, **or alternatively** via the terminal:
```bash
security create-keychain -p '' login.keychain
security unlock-keychain -p '' login.keychain
security login-keychain -s login.keychain
```
*Note:* On the Mac mini, all three commands needed to be run (the password can be left empty) because the existing keychain file was a `.db` file (a newer format) which caused issues with the `security` command. 
The keychain was placed in `~/Library/Keychains/` (for the ubuntu user).
If the host restarts the unlock and login commands need to be re-run.

### If the VM stops working
If you get the following error in Kubernetes:
```
Warning  Failed  9s  macos-vz-kubelet  spec.containers{macos}: Failed to start container macos: Error Domain=VZErrorDomain Code=1 Description="Internal Virtualization error. The virtual machine failed to start." UserInfo={
    NSLocalizedFailure = "Internal Virtualization error.";
    NSLocalizedFailureReason = "The virtual machine failed to start.";
}
```
Run the following in `~/Library/Keychains/`:
```bash
security unlock-keychain login.keychain
security login-keychain -s login.keychain
```

## Namespace & Pod Tolerations
To allow Pods to be scheduled on the virtual kubelet node, the target Kubernetes namespace must have the appropriate toleration added to its `tolerationsWhitelist` (this can be done manually for now, but should ideally be automated during namespace creation).

Pods deployed to the virtual node require the following toleration:
```json
{
  "key": "virtual-kubelet.io/provider",
  "operator": "Exists",
  "effect": "NoSchedule"
}
```
*Note: If your cluster runs multiple virtual-kubelet providers (e.g., both macOS nodes and AWS Fargate), you should use stricter targeting by setting `"operator": "Equal"` and `"value": "macos-vz"`. This ensures macOS workloads are specifically routed to Mac nodes rather than landing on unrelated virtual nodes.*
