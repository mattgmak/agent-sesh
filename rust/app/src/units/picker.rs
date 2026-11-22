//! Glue from the front door to `picker-rs`: parse the arguments, call `picker_rs::port`,
//! print what the original prints and return its exit code. A case the port does not
//! cover yet goes back with `crate::original::run(&argv[1..])`, or
//! `crate::original::run_with_stdin(&argv[1..], &input)` once stdin was read.

use std::process::ExitCode;

use clap::ArgMatches;

pub fn run(command: &str, _matches: &ArgMatches) -> ExitCode {
    let argv: Vec<String> = std::env::args().collect();
    match command {
        "list" => {
            // Go: every argument after `list` must be exactly `--json`; anything
            // else prints usage and exits 2 (repeats of --json are harmless).
            let mut json = false;
            for arg in argv.iter().skip(2) {
                if arg == "--json" {
                    json = true;
                } else {
                    eprintln!("usage: agent-sesh list [--json]");
                    return ExitCode::from(2);
                }
            }
            match picker_rs::port::list(json) {
                Ok(out) => {
                    print!("{out}");
                    ExitCode::SUCCESS
                }
                Err(e) => {
                    eprintln!("{e}");
                    ExitCode::FAILURE
                }
            }
        }
        "preview" => {
            // Go: `preview` with no argument prints usage and exits 2; extra
            // arguments after the key are ignored (only os.Args[2] is read).
            let Some(key) = argv.get(2) else {
                eprintln!("usage: agent-sesh preview <session-id>");
                return ExitCode::from(2);
            };
            match picker_rs::port::preview(key.clone()) {
                Ok(out) => {
                    print!("{out}");
                    ExitCode::SUCCESS
                }
                Err(e) => {
                    eprintln!("{e}");
                    ExitCode::FAILURE
                }
            }
        }
        _ => {
            let os_argv: Vec<std::ffi::OsString> = std::env::args_os().collect();
            crate::original::run(&os_argv[1..])
        }
    }
}
