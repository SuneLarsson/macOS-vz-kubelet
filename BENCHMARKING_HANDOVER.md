# AI Benchmarking Suite: Handover & Context

This document provides context for the AI/ML benchmarking suite developed during our session on the `macos-exec-kubelet` repository. Since the suite has now been copied to the `macos-vz-kubelet` (Virtualization) repository, this document outlines what the suite does, how it is architected, and the specific technical requirements the virtualization layer must support to run these tests accurately.

## 1. Context & Objectives

The primary objective was to build a rigorous, statistically significant benchmarking suite to evaluate hardware-native AI/ML performance on Apple Silicon (via `macos-exec-kubelet`) and establish a baseline comparison against bare-metal Linux workers. The ultimate goal of porting this suite to the `macos-vz-kubelet` is to gather comparative data on the exact performance degradation penalty incurred by hardware virtualization (VMs).

The suite is designed to be triggered via standard Kubernetes Jobs. All Python scripts run 1000 iterations to extract exact percentiles (P10, P50, P90, P95, P99), mean, max, and min execution times, saving structured JSON logs securely to an NFS/Ceph mount for plotting in a Master's Thesis.

## 2. What Was Accomplished (The Suite)

The benchmarking suite leverages a unified bash execution engine (`run_ai_suite.sh`) to detect the host architecture (Linux vs. Darwin) dynamically, provision local ephemeral environments, and sequentially execute the following tests:

- **LLM Inference (`llm_inference_bench.py`)**: Uses the `mlx-lm` framework to reproduce the methodology of recent academic research. It steps through a strict list of 4-bit quantized models (`Llama-3.2-1B`, `Qwen2.5-1.5B`, `Mistral-7B`, etc.) to measure exact Tokens-Per-Second (TPS) and Time-to-First-Token (TTFT) natively on unified memory.
- **Hardware Compute Verification (`coreml_hardware_bench.py`)**: Dynamically compiles a heavy CoreML model and evaluates execution time iteratively over three distinct hardware targets: `CPU_ONLY`, `CPU_AND_GPU`, and `ALL` (forcing Apple Neural Engine). This matrix is essential for proving hardware acceleration profiles.
- **Platform Agnostic Baseline (`cross_platform_ai_bench.py`)**: Uses PyTorch tensor multiplication to explicitly map to NVIDIA `CUDA` on Linux and Apple `MPS` on Mac, yielding a 1:1 hardware comparison framework.
- **Micro Operations (`TristanBilot/mlx-benchmark`)**: Programmatically clones and fires a comprehensive third-party profiling suite to log micro-latency for array math operations.
- **Storage/Data Plumbing (`nfs_data_bench.py`)**: Analyzes the specific operational penalties of streaming training data securely over a network NFS mount versus saturating the local NVMe drive locally with preloaded batches.

**Crucial Optimizations Implemented:** 
To prevent Kubernetes and Ceph storage from crashing the benchmark, the HuggingFace Hub cache directory (`HF_HOME`) is strictly redirected to the fast, local `/tmp` drive across both machines. This completely bypasses severe POSIX `filelock` deadlocks common with macOS NFS clients while securing blazing-fast model loading times without re-downloading multi-gigabytes of matrices per pod lifespan.

## 3. Next Steps for Virtualization (`macos-vz-kubelet`)

To successfully run this suite inside a macOS guest VM managed by Apple's `Virtualization.framework` (which powers the `vz` kubelet), the virtualization implementation must account for the following architectural constraints:

### A. Memory Allocation (Crucial)
The heaviest 4-bit quantized MLX models (e.g., Mistral 7B) require a peak memory ceiling of roughly 10-12GB to operate gracefully. **The VM guest environment MUST be provisioned with at least 14-16GB of dedicated RAM.** If the VM is restricted to 8GB, the OS will violently swap the ML weights to the disk pagefile. The benchmark results will explicitly reflect storage I/O paging latency rather than true CPU/GPU processing capabilities, invalidating the thesis metrics.

### B. GPU and Neural Engine (ANE) Passthrough
- **GPU**: The `gpu_mlx_test.py` and PyTorch `mps` scripts require the Metal framework. The virtualization layer must forcefully allocate Mac graphics resources to the VM (Paravirtualized Graphics via `vz`). 
- **ANE**: The `coreml_hardware_bench.py` explicitly tests the Neural Engine (`ComputeUnit.ALL`). Depending on the macOS host/guest version combination, `Virtualization.framework` historically struggles to faithfully pass ANE chips entirely through to the guest environment. The JSON output of this test will programmatically prove for your thesis whether your VM is utilizing the Neural Engine or silently falling back to the guest GPU/CPU.

### C. Persistent `/tmp` Storage for Cache Acceleration
Because we re-routed `HF_HOME="/tmp/ai_bench_hf_cache"` to bypass network deadlocks, the massive model `.safetensor` files will stream directly into the VM's local `/tmp` directory. 
- Ensure that the VM image partitions enough raw disk space (at least 20-30GB of free space) so `mlx-lm` doesn't crash from a `Disk Full` error while caching models.
- If `/private/tmp` is mapped to a virtio block device (`virtio-blk`), the script execution will cleanly and accurately measure VM storage emulation overhead compared to native execution.

### D. NFS Mount Bridging
Just like `macos-exec-kubelet`, the virtual job runner must properly mount the Ceph NFS network share securely into the guest OS. `run_ai_suite.sh` expects to run strictly *from* this unified mount, and saves the JSON results dynamically to `$SCRIPT_DIR/results/`. Ensure the guest VM has recursive Read/Write permissions to the mounted folder to avoid `Permission Denied` errors during JSON serialization.

---
**Summary:** You are now equipped with an incredibly robust mechanism for proving hardware execution parity. Boot the suite exactly as-is on the virtual architecture, and compare the JSON distributions directly against your `macos-exec-kubelet` metrics!
