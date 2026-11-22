//! Glue from the front door to `version-rs`: parse the arguments, call `version_rs::port`,
//! print what the original prints and return its exit code. A case the port does not
//! cover yet goes back with `crate::original::run(&argv[1..])`, or
//! `crate::original::run_with_stdin(&argv[1..], &input)` once stdin was read.

use std::io::Write;
use std::process::ExitCode;

use clap::ArgMatches;

pub fn run(_command: &str, _matches: &ArgMatches) -> ExitCode {
    match version_rs::port::string() {
        Ok(version) => {
            let stdout = std::io::stdout();
            let mut out = stdout.lock();
            match writeln!(out, "agent-sesh {version}") {
                Ok(()) => ExitCode::SUCCESS,
                Err(e) => {
                    eprintln!("{e}");
                    ExitCode::FAILURE
                }
            }
        }
        Err(e) => {
            eprintln!("{e}");
            ExitCode::FAILURE
        }
    }
}
