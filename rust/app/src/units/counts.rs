//! Glue from the front door to `counts-rs`: parse the arguments, call `counts_rs::port`,
//! print what the original prints and return its exit code. A case the port does not
//! cover yet goes back with `crate::original::run(&argv[1..])`, or
//! `crate::original::run_with_stdin(&argv[1..], &input)` once stdin was read.

use std::process::ExitCode;

use clap::ArgMatches;
use serde_json::json;

const USAGE: &str = "usage: agent-sesh counts [--json] [--format <template>]";

pub fn run(_command: &str, _matches: &ArgMatches) -> ExitCode {
    // Go `flag` semantics: `--json`, `--format V`, `--format=V`; anything else is usage.
    let argv: Vec<String> = std::env::args().collect();
    let mut json = false;
    let mut format = String::new();
    let mut i = 2; // argv[0] is the program name, argv[1] is "counts"
    while i < argv.len() {
        match argv[i].as_str() {
            "--json" | "-json" => json = true,
            "--format" | "-format" => {
                i += 1;
                match argv.get(i) {
                    Some(value) => format = value.clone(),
                    None => {
                        eprintln!("{USAGE}");
                        return ExitCode::from(2);
                    }
                }
            }
            arg if arg.starts_with("--format=") => format = arg["--format=".len()..].to_string(),
            arg if arg.starts_with("-format=") => format = arg["-format=".len()..].to_string(),
            _ => {
                eprintln!("{USAGE}");
                return ExitCode::from(2);
            }
        }
        i += 1;
    }
    if json && !format.is_empty() {
        eprintln!("agent-sesh counts: --json and --format are mutually exclusive");
        return ExitCode::from(2);
    }
    let opts = json!({ "json": json, "format": format });
    match counts_rs::port::run(&opts) {
        Ok(out) => {
            print!("{out}");
            ExitCode::SUCCESS
        }
        // A template syntax the mini-renderer does not cover: let the original answer.
        Err(ref e) if e.starts_with("rust-anything:") => {
            let os_argv: Vec<std::ffi::OsString> = std::env::args_os().collect();
            crate::original::run(&os_argv[1..])
        }
        Err(e) => {
            eprintln!("{e}");
            ExitCode::FAILURE
        }
    }
}
