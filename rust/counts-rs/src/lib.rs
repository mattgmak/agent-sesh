//! The Go bridge for `internal/counts/`: a JSON dispatcher over the port, and its entry
//! point.

#[cfg(feature = "ffi")]
use std::ffi::{CStr, CString, c_char};

use serde_json::{Value, json};

pub mod port;

/// Argument `i`, decoded from JSON into the port's parameter type.
fn arg<T: serde::de::DeserializeOwned>(args: &[Value], i: usize) -> Result<T, String> {
    serde_json::from_value(args.get(i).cloned().unwrap_or(Value::Null))
        .map_err(|e| format!("argument {i}: {e}"))
}

fn dispatch(name: &str, args: &[Value]) -> Result<Value, String> {
    match name {
        "LoadAndCount" => port::load_and_count(),
        "Write" => {
            let opts = arg(args, 0)?;
            let counts = arg(args, 1)?;
            Ok(json!(port::write(&opts, &counts)?))
        }
        "Run" => {
            let opts = arg(args, 0)?;
            Ok(json!(port::run(&opts)?))
        }
        _ => Err(format!("no function `{name}`")),
    }
}

/// Runs `name` on the JSON array `args`; never unwinds into the caller.
pub fn call_json(name: &str, args: &str) -> String {
    let result = std::panic::catch_unwind(|| {
        let args: Vec<Value> = serde_json::from_str(args).map_err(|e| e.to_string())?;
        dispatch(name, &args)
    });
    match result {
        Ok(Ok(value)) => json!({"ok": value}),
        Ok(Err(message)) => json!({"error": message}),
        Err(panic) => {
            let message = panic
                .downcast_ref::<&str>()
                .map(ToString::to_string)
                .or_else(|| panic.downcast_ref::<String>().cloned())
                .unwrap_or_else(|| "panic".into());
            json!({"panic": message})
        }
    }
    .to_string()
}

/// The C entry point: `char *rust_anything_call(const char *name, const char *args)`. Free the
/// answer with `rust_anything_free`.
///
/// # Safety
///
/// `name` and `args` must be NUL-terminated strings, valid for the call.
// SAFETY: exported unmangled under a name only rust-anything defines.
#[unsafe(no_mangle)]
// SAFETY: callers uphold the contract under `# Safety`.
#[cfg(feature = "ffi")]
pub unsafe extern "C" fn rust_anything_call(
    name: *const c_char,
    args: *const c_char,
) -> *mut c_char {
    let text = |ptr: *const c_char| -> String {
        if ptr.is_null() {
            return String::new();
        }
        // SAFETY: the caller passes NUL-terminated strings that live for the call (see above).
        unsafe { CStr::from_ptr(ptr) }
            .to_string_lossy()
            .into_owned()
    };
    let answer = call_json(&text(name), &text(args));
    // A JSON text never contains NUL: serde_json escapes it as \u0000.
    CString::new(answer).map_or(std::ptr::null_mut(), CString::into_raw)
}

/// Frees an answer of `rust_anything_call`.
///
/// # Safety
///
/// `ptr` must come from `rust_anything_call` and not be freed before; NULL is ignored.
// SAFETY: exported unmangled under a name only rust-anything defines.
#[unsafe(no_mangle)]
// SAFETY: callers uphold the contract under `# Safety`.
#[cfg(feature = "ffi")]
pub unsafe extern "C" fn rust_anything_free(ptr: *mut c_char) {
    if !ptr.is_null() {
        // SAFETY: `ptr` was made by `CString::into_raw` in `rust_anything_call` (see above).
        drop(unsafe { CString::from_raw(ptr) });
    }
}
