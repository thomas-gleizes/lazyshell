mod app;
mod client;
mod config;
mod daemon;
mod launcher;
mod paths;
mod protocol;
mod search;

use std::path::PathBuf;

use anyhow::Result;
use clap::{Args as ClapArgs, Parser, Subcommand};

use client::Client;
use daemon::{Command, State, Target};

/// Gestionnaire de processus shell en TUI : sans sous-commande, ouvre la TUI du daemon du projet.
#[derive(Parser)]
#[command(args_conflicts_with_subcommands = true)]
struct Args {
    /// Fichier de config, ou dossier contenant lazyshell.yml (défaut : dossier courant).
    path: Option<PathBuf>,
    #[command(subcommand)]
    command: Option<Cli>,
}

#[derive(Subcommand)]
enum Cli {
    /// Lance le daemon du projet au premier plan (la TUI le lance seule en arrière-plan).
    Daemon {
        path: Option<PathBuf>,
        /// S'arrête seul quand plus aucune session n'est active et qu'aucun client n'est connecté.
        #[arg(long)]
        auto_exit: bool,
    },
    /// Affiche l'état des sessions du daemon.
    Status { path: Option<PathBuf> },
    /// Lance une session ou un groupe.
    Start(TargetArgs),
    /// Arrête une session ou un groupe.
    Stop(TargetArgs),
    /// Relance une session ou un groupe.
    Restart(TargetArgs),
    /// Relit le fichier de config et l'applique au daemon en cours.
    Reload { path: Option<PathBuf> },
    /// Arrête toutes les sessions puis le daemon.
    Shutdown { path: Option<PathBuf> },
}

#[derive(ClapArgs)]
struct TargetArgs {
    /// Nom de la session (ou du groupe avec --group).
    name: String,
    #[arg(short, long)]
    group: bool,
    path: Option<PathBuf>,
}

impl TargetArgs {
    fn target(&self) -> Target {
        if self.group { Target::Group(self.name.clone()) } else { Target::Session(self.name.clone()) }
    }
}

fn main() -> Result<()> {
    let args = Args::parse();
    let Some(cli) = args.command else {
        let file = config::Config::resolve_file(&args.path.unwrap_or_else(|| ".".into()))?;
        let socket = paths::socket_for(&file)?;
        launcher::ensure_daemon(&file, &socket)?;
        return app::run(&socket);
    };

    match cli {
        Cli::Daemon { path, auto_exit } => {
            let path = path.unwrap_or_else(|| ".".into());
            let file = config::Config::resolve_file(&path)?;
            let socket = paths::socket_for(&file)?;
            let (config, base) = config::Config::load(&path)?;
            daemon::serve(&config, &base, &file, &socket, auto_exit)
        }
        Cli::Status { path } => print_status(&mut connect(path)?),
        Cli::Start(t) => connect(t.path.clone())?.command(Command::Start(t.target())),
        Cli::Stop(t) => connect(t.path.clone())?.command(Command::Stop(t.target())),
        Cli::Restart(t) => connect(t.path.clone())?.command(Command::Restart(t.target())),
        Cli::Reload { path } => {
            println!("{}", connect(path)?.reload()?.summary());
            Ok(())
        }
        Cli::Shutdown { path } => connect(path)?.shutdown(),
    }
}

fn connect(path: Option<PathBuf>) -> Result<Client> {
    let file = config::Config::resolve_file(&path.unwrap_or_else(|| ".".into()))?;
    Client::connect(&paths::socket_for(&file)?)
}

fn print_status(client: &mut Client) -> Result<()> {
    for info in client.list()? {
        let state = match info.state {
            State::NotStarted => "arrêtée".to_string(),
            State::Running => format!("en cours ({}s)", info.uptime.as_secs()),
            State::Stopping => "arrêt…".to_string(),
            State::Exited(0) => "terminée".to_string(),
            State::Exited(code) => format!("échec ({code})"),
        };
        println!("{:<14} {:<20} {state}", info.group, info.name);
    }
    Ok(())
}
