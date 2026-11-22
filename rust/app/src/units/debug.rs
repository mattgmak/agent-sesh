//! Glue from the front door to `debug-rs`: parse the arguments, call `debug_rs::port`,
//! print what the original prints and return its exit code. A case the port does not
//! cover yet goes back with `crate::original::run(&argv[1..])`, or
//! `crate::original::run_with_stdin(&argv[1..], &input)` once stdin was read.

use std::process::ExitCode;

use clap::ArgMatches;

const USAGE: &str = "usage: agent-sesh debug <registry|validate|panes|discover|pane <target>>";

fn emit(result: Result<String, String>) -> ExitCode {
    match result {
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

pub fn run(_command: &str, _matches: &ArgMatches) -> ExitCode {
    let argv: Vec<String> = std::env::args().collect();
    let Some(sub) = argv.get(2) else {
        eprintln!("{USAGE}");
        return ExitCode::from(2);
    };
    match sub.as_str() {
        "registry" => emit(debug_rs::port::registry()),
        "validate" => emit(debug_rs::port::validate()),
        "panes" => emit(debug_rs::port::panes()),
        "discover" => emit(debug_rs::port::discover()),
        "pane" => match argv.get(3) {
            Some(target) => emit(debug_rs::port::pane(target.clone())),
            None => {
                eprintln!("usage: agent-sesh debug pane <tmux-target>");
                ExitCode::from(2)
            }
        },
        _ => {
            eprintln!("{USAGE}");
            ExitCode::from(2)
        }
    }
}
