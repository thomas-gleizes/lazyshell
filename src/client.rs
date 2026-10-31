use std::os::unix::net::UnixStream;
use std::path::Path;
use std::sync::mpsc::{self, Receiver};
use std::thread;

use anyhow::{Context, Result, bail};

use crate::daemon::{Command, ReloadReport, SessionInfo};
use crate::paths;
use crate::protocol::{
    Request, Response, VERSION, read_handshake, read_message, write_handshake, write_message,
};

pub struct Client {
    stream: UnixStream,
}

impl Client {
    pub fn connect(socket: &Path) -> Result<Self> {
        let mut stream = UnixStream::connect(socket)
            .with_context(|| "aucun daemon actif pour ce projet (la TUI en lance un automatiquement)")?;
        write_handshake(&mut stream)?;
        let daemon_version = read_handshake(&mut stream).context("handshake avec le daemon")?;
        if daemon_version != VERSION {
            let pid = std::fs::read_to_string(paths::lock_for(socket)).unwrap_or_default();
            let hint = match pid.trim() {
                "" => String::new(),
                pid => format!(" : arrêter l'ancien daemon avec `kill {pid}`"),
            };
            bail!("daemon en protocole v{daemon_version}, client en v{VERSION}{hint}");
        }
        Ok(Client { stream })
    }

    pub fn list(&mut self) -> Result<Vec<SessionInfo>> {
        match self.call(Request::List)? {
            Response::Infos(infos) => Ok(infos),
            other => bail!("réponse inattendue : {other:?}"),
        }
    }

    pub fn command(&mut self, command: Command) -> Result<()> {
        self.expect_ok(Request::Command(command))
    }

    pub fn resize(&mut self, session: &str, rows: u16, cols: u16) -> Result<()> {
        self.expect_ok(Request::Resize { session: session.to_string(), rows, cols })
    }

    pub fn env(&mut self, session: &str) -> Result<Vec<(String, String)>> {
        match self.call(Request::Env { session: session.to_string() })? {
            Response::Env(vars) => Ok(vars),
            other => bail!("réponse inattendue : {other:?}"),
        }
    }

    pub fn reload(&mut self) -> Result<ReloadReport> {
        match self.call(Request::Reload)? {
            Response::Reloaded(report) => Ok(report),
            other => bail!("réponse inattendue : {other:?}"),
        }
    }

    pub fn shutdown(&mut self) -> Result<()> {
        self.expect_ok(Request::Shutdown)
    }

    /// Ouvre une connexion dédiée au flux de sortie d'une session.
    pub fn subscribe(socket: &Path, session: &str) -> Result<Stream> {
        let mut client = Client::connect(socket)?;
        let run_id = match client.call(Request::Subscribe { session: session.to_string() })? {
            Response::Subscribed { run_id } => run_id,
            other => bail!("réponse inattendue : {other:?}"),
        };
        let mut reader = client.stream.try_clone()?;
        let (tx, rx) = mpsc::channel();
        thread::spawn(move || {
            loop {
                let event = match read_message::<Response>(&mut reader) {
                    Ok(Some(Response::Output(data))) => StreamEvent::Data(data),
                    Ok(Some(Response::Ping)) => continue,
                    // StreamEnd, connexion fermée ou erreur : la sortie est terminée.
                    _ => StreamEvent::End,
                };
                let end = matches!(event, StreamEvent::End);
                if tx.send(event).is_err() || end {
                    break;
                }
            }
        });
        Ok(Stream { run_id, rx, socket: client.stream })
    }

    fn expect_ok(&mut self, request: Request) -> Result<()> {
        match self.call(request)? {
            Response::Ok => Ok(()),
            other => bail!("réponse inattendue : {other:?}"),
        }
    }

    /// Envoie une requête et attend sa réponse ; `Response::Error` devient une erreur.
    fn call(&mut self, request: Request) -> Result<Response> {
        write_message(&mut self.stream, &request)?;
        match read_message(&mut self.stream)?.context("le daemon a fermé la connexion")? {
            Response::Error(message) => bail!("{message}"),
            response => Ok(response),
        }
    }
}

pub enum StreamEvent {
    Data(Vec<u8>),
    End,
}

/// Flux de sortie d'une exécution : l'historique d'abord, puis le direct.
pub struct Stream {
    pub run_id: u64,
    rx: Receiver<StreamEvent>,
    socket: UnixStream,
}

impl Stream {
    /// Attend le prochain événement ; `None` si le flux est rompu (équivaut à une fin).
    pub fn recv(&self) -> Option<StreamEvent> {
        self.rx.recv().ok()
    }

    pub fn try_recv(&self) -> Option<StreamEvent> {
        self.rx.try_recv().ok()
    }
}

impl Drop for Stream {
    /// Ferme la connexion : le thread de lecture se débloque et le daemon libère l'abonné.
    fn drop(&mut self) {
        let _ = self.socket.shutdown(std::net::Shutdown::Both);
    }
}
