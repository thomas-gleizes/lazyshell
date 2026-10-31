mod app;
mod client;
mod config;
mod daemon;
mod envfile;
mod i18n;
mod launcher;
mod paths;
mod plain;
mod protocol;
mod search;
mod settings;

use std::io::{ErrorKind, Write};
use std::path::PathBuf;
use std::thread;
use std::time::{Duration, Instant};

use anyhow::{Result, bail};
use clap::{Args as ClapArgs, Parser, Subcommand};

use client::{Client, StreamEvent};
use daemon::{Command, SessionInfo, State, Target};

/// Le daemon laisse 10 s à une session avant le SIGKILL.
const KILL_WAIT: Duration = Duration::from_secs(15);

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
    /// Liste les sessions en cours de tous les daemons, avec leur config (comme `docker ps`).
    Ps {
        /// Inclut aussi les sessions arrêtées ou terminées.
        #[arg(short, long)]
        all: bool,
    },
    /// Arrête toutes les sessions en cours d'une config (défaut : dossier courant).
    Kill {
        path: Option<PathBuf>,
        /// Vise les sessions de toutes les configs.
        #[arg(short, long, conflicts_with = "path")]
        all: bool,
    },
    /// Lance une session ou un groupe.
    Start(TargetArgs),
    /// Arrête une session ou un groupe.
    Stop(TargetArgs),
    /// Relance une session ou un groupe.
    Restart(TargetArgs),
    /// Affiche la sortie d'une session sur stdout (texte brut), pour un script ou un agent.
    Logs {
        /// Nom de la session.
        name: String,
        path: Option<PathBuf>,
        /// Reste à l'écoute de la suite, comme `tail -f` (s'arrête quand l'exécution se termine).
        #[arg(short, long)]
        follow: bool,
        /// Se limite aux N dernières lignes de l'historique (défaut : tout l'historique conservé).
        #[arg(short = 'n', long, value_name = "N")]
        lines: Option<usize>,
        /// Conserve les séquences d'échappement ANSI (couleurs, curseur) telles que émises.
        #[arg(long)]
        raw: bool,
    },
    /// Affiche les variables d'environnement injectées dans une session (`CLÉ=valeur`).
    Env {
        /// Nom de la session.
        name: String,
        path: Option<PathBuf>,
    },
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
    i18n::init(settings::Settings::load()?.language);
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
        Cli::Ps { all } => print_ps(all),
        Cli::Kill { path, all } => kill(path, all),
        Cli::Status { path } => print_status(&mut connect(path)?),
        Cli::Start(t) => connect(t.path.clone())?.command(Command::Start(t.target())),
        Cli::Stop(t) => connect(t.path.clone())?.command(Command::Stop(t.target())),
        Cli::Restart(t) => connect(t.path.clone())?.command(Command::Restart(t.target())),
        Cli::Reload { path } => {
            println!("{}", connect(path)?.reload()?.summary());
            Ok(())
        }
        Cli::Shutdown { path } => connect(path)?.shutdown(),
        Cli::Logs { name, path, follow, lines, raw } => logs(&name, path, follow, lines, raw),
        Cli::Env { name, path } => {
            let mut out = std::io::stdout().lock();
            for (key, value) in connect(path)?.env(&name)? {
                if writeln!(out, "{key}={value}").is_err() {
                    break;
                }
            }
            Ok(())
        }
    }
}

/// Écrit la sortie d'une session sur stdout : l'historique (ou ses `lines` dernières lignes),
/// puis, avec `follow`, la suite en direct jusqu'à la fin de l'exécution.
fn logs(name: &str, path: Option<PathBuf>, follow: bool, lines: Option<usize>, raw: bool) -> Result<()> {
    let file = config::Config::resolve_file(&path.unwrap_or_else(|| ".".into()))?;
    let stream = Client::subscribe(&paths::socket_for(&file)?, name)?;
    let mut stripper = plain::Stripper::new();
    let mut out = std::io::stdout().lock();
    let mut first = true;
    // Le premier événement est l'historique ; les suivants sont le direct.
    while let Some(StreamEvent::Data(data)) = stream.recv() {
        let text = if raw { data } else { stripper.feed(&data) };
        let text = match (first, lines) {
            (true, Some(n)) => plain::last_lines(&text, n),
            _ => &text,
        };
        let written = out.write_all(text).and_then(|()| out.flush());
        match written {
            Ok(()) => {}
            // `lazyshell logs … | head` : le lecteur est parti, ce n'est pas une erreur.
            Err(err) if err.kind() == ErrorKind::BrokenPipe => return Ok(()),
            Err(err) => return Err(err.into()),
        }
        first = false;
        // Sans -f, on s'arrête après l'historique.
        if !follow {
            break;
        }
    }
    Ok(())
}

fn connect(path: Option<PathBuf>) -> Result<Client> {
    let file = config::Config::resolve_file(&path.unwrap_or_else(|| ".".into()))?;
    Client::connect(&paths::socket_for(&file)?)
}

fn print_status(client: &mut Client) -> Result<()> {
    for info in client.list()? {
        println!("{:<14} {:<20} {}", info.group, info.name, state_label(&info));
    }
    Ok(())
}

fn state_label(info: &SessionInfo) -> String {
    match info.state {
        State::NotStarted => i18n::stopped().to_string(),
        State::Running => i18n::running(info.uptime.as_secs()),
        State::Stopping => i18n::stopping().to_string(),
        State::Exited(0) => i18n::finished().to_string(),
        State::Exited(code) => i18n::failed(code),
    }
}

fn is_active(info: &SessionInfo) -> bool {
    matches!(info.state, State::Running | State::Stopping)
}

/// Daemons joignables : (fichier de config, client). Les sockets orphelines sont ignorées.
fn live_daemons() -> Result<Vec<(PathBuf, Client)>> {
    Ok(paths::known_daemons()?
        .into_iter()
        .filter_map(|(config, socket)| Client::connect(&socket).ok().map(|client| (config, client)))
        .collect())
}

/// Remplace le dossier personnel par `~` pour garder les lignes courtes.
fn display_path(path: &std::path::Path) -> String {
    match std::env::var_os("HOME").map(PathBuf::from).and_then(|home| path.strip_prefix(home).ok().map(|p| p.to_path_buf())) {
        Some(rest) => format!("~/{}", rest.display()),
        None => path.display().to_string(),
    }
}

fn print_ps(all: bool) -> Result<()> {
    let mut rows = Vec::new();
    for (config, mut client) in live_daemons()? {
        let config = display_path(&config);
        for info in client.list()? {
            if all || is_active(&info) {
                rows.push((config.clone(), info));
            }
        }
    }
    if rows.is_empty() {
        println!("{}", i18n::no_session_running());
        return Ok(());
    }
    let width = rows.iter().map(|(config, _)| config.chars().count()).max().unwrap_or(0).max("CONFIG".len());
    println!("{}", i18n::ps_header(width));
    for (config, info) in rows {
        println!("{config:<width$}  {:<14} {:<20} {}", info.group, info.name, state_label(&info));
    }
    Ok(())
}

/// Arrête les sessions en cours de la config visée (ou de toutes avec `--all`) et attend
/// qu'elles soient réellement terminées (SIGTERM, puis SIGKILL côté daemon après un délai).
fn kill(path: Option<PathBuf>, all: bool) -> Result<()> {
    let mut daemons = if all {
        live_daemons()?
    } else {
        let file = config::Config::resolve_file(&path.unwrap_or_else(|| ".".into()))?;
        vec![(file.clone(), Client::connect(&paths::socket_for(&file)?)?)]
    };

    let mut killed = 0;
    for (config, client) in &mut daemons {
        let active: Vec<_> = client.list()?.into_iter().filter(is_active).collect();
        for info in &active {
            client.command(Command::Stop(Target::Session(info.name.clone())))?;
        }
        let deadline = Instant::now() + KILL_WAIT;
        while client.list()?.iter().any(is_active) {
            if Instant::now() >= deadline {
                bail!("{} : des sessions ne se sont pas arrêtées", display_path(config));
            }
            thread::sleep(Duration::from_millis(100));
        }
        for info in &active {
            println!("{}", i18n::session_stopped(&display_path(config), &info.name));
        }
        killed += active.len();
    }
    if killed == 0 {
        println!("{}", i18n::no_session_running());
    }
    Ok(())
}
