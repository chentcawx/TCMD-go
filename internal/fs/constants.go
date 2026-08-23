// Package fs — project-wide constants for the filesystem layer.
package fs

// copyBufSize is the chunk used while streaming file copies, so a multi-GB
// file never spikes heap by being read whole. Tuned for typical USB/SSD
// throughput: large enough to amortize syscalls, small enough to keep the
// per-file progress callback responsive.
const copyBufSize = 32 * 1024 // 32 KiB
