use std::ffi::OsString;

use clap::{Arg, ArgAction, Command, value_parser};

/// The command `units::dispatch` gets when argv names no subcommand.
pub const ROOT: &str = "agent-sesh";

fn rest() -> Arg {
    Arg::new("args")
        .num_args(0..)
        .trailing_var_arg(true)
        .allow_hyphen_values(true)
        .value_parser(value_parser!(OsString))
}

/// Routes argv to a command. Anything this parser rejects runs the original, so it never
/// has to match every way the original accepts its arguments.
pub fn command() -> Command {
    Command::new(ROOT)
        .disable_help_flag(true)
        .disable_version_flag(true)
        .disable_help_subcommand(true)
        .args_conflicts_with_subcommands(true)
        .arg(rest())
        .subcommand(
            Command::new("counts")
                .disable_help_flag(true)
                .arg(Arg::new("format").long("format").action(ArgAction::Count))
                .arg(Arg::new("json").long("json").action(ArgAction::Count))
                .arg(rest())
        )
        .subcommand(
            Command::new("debug")
                .disable_help_flag(true)
                .arg(rest())
        )
        .subcommand(
            Command::new("list")
                .disable_help_flag(true)
                .arg(Arg::new("json").long("json").action(ArgAction::Count))
                .arg(rest())
        )
        .subcommand(
            Command::new("notify")
                .disable_help_flag(true)
                .arg(Arg::new("backend").long("backend").action(ArgAction::Count))
                .arg(Arg::new("body").long("body").action(ArgAction::Count))
                .arg(Arg::new("event").long("event").action(ArgAction::Count))
                .arg(Arg::new("session").long("session").action(ArgAction::Count))
                .arg(Arg::new("title").long("title").action(ArgAction::Count))
                .arg(rest())
        )
        .subcommand(
            Command::new("preview")
                .disable_help_flag(true)
                .arg(rest())
        )
        .subcommand(
            Command::new("version")
                .disable_help_flag(true)
                .alias("--version")
                .alias("-v")
                .arg(rest())
        )
}
