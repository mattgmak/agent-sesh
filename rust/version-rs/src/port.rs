//! The Rust port of `internal/version/`.
//!
//! Replace each `unimplemented!` with the port of the source function. Return `Err` where
//! the source throws or raises: the caller gets an error there too. After changing a
//! signature, run `rust-anything refresh` to regenerate the bindings. `rust-anything diff` checks
//! behavior against what the source recorded.

#![forbid(unsafe_code)]

/// Build-time version stamp (ADR-005); mirrors the Go ldflags variable.
const VERSION: &str = match option_env!("AGENT_SESH_VERSION") {
    Some(v) => v,
    None => "dev",
};

/// Port of `String` (`internal/version/version.go:16`).
pub fn string() -> Result<String, String> {
    Ok(VERSION.to_string())
}
