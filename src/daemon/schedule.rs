//! Démarrage des sessions dans l'ordre de leurs dépendances (`depends_on`).
//!
//! Une demande de démarrage ([`Manager::request`]) met la session et les dépendances à
//! lancer en attente, puis démarre tout de suite ce qui peut l'être. Le reste est repris
//! à chaque `tick`. Rien ne se propage dans l'autre sens : arrêter ou relancer une
//! dépendance ne touche pas les sessions qui en dépendent.

use std::collections::{HashSet, VecDeque};

use anyhow::{Result, anyhow};
use serde::{Deserialize, Serialize};

use super::manager::Manager;
use super::probe::Readiness;
use super::session::Status;
use crate::config::Kind;

/// Ce qui retient le démarrage d'une session.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum Wait {
    /// Dépendances pas encore satisfaites, mais en bonne voie.
    On(Vec<String>),
    /// Une dépendance a échoué ; la session démarrera si elle finit par être satisfaite.
    Blocked { dependency: String, cause: Cause },
    /// Les dépendances étaient satisfaites mais le processus n'a pas pu être créé.
    LaunchFailed(String),
}

/// Pourquoi une dépendance bloque.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum Cause {
    /// Jamais lancée, ou tâche arrêtée à la demande même si elle est sortie avec le code 0.
    NotStarted,
    Exited(i32),
    /// La sonde `ready` a expiré.
    NotReady,
    /// Elle est elle-même bloquée.
    Blocked,
    /// Tâche réussie avec une définition qui a changé depuis.
    Changed,
}

enum Verdict {
    Satisfied,
    Pending,
    Failed(Cause),
}

impl Manager {
    /// Demande le démarrage des sessions `roots` et, en chaîne, de leurs dépendances.
    /// Une session active n'est pas relancée ; sa sonde expirée est rejouée. Une tâche
    /// visée est toujours rejouée ; une tâche simplement requise ne l'est pas si elle a
    /// déjà réussi à l'identique, mais ses propres dépendances sont tout de même lancées.
    /// Renvoie le premier échec de lancement parmi les sessions visées et leurs
    /// dépendances.
    pub(super) fn request(&mut self, roots: &[usize]) -> Result<()> {
        let mut queue = Vec::new();
        for &index in roots {
            let session = &mut self.sessions[index];
            if session.is_active() {
                session.rearm_probe();
            } else {
                session.wait = Some(Wait::On(Vec::new()));
                queue.push(index);
            }
        }
        // Les sessions concernées par cette demande : les visées et ce qu'elles requièrent.
        let mut involved: HashSet<usize> = roots.iter().copied().collect();
        let mut visited = HashSet::new();
        while let Some(index) = queue.pop() {
            if !visited.insert(index) {
                continue;
            }
            for dependency in self.dependencies(index) {
                involved.insert(dependency);
                let session = &mut self.sessions[dependency];
                match session.status() {
                    // En cours d'arrêt : elle sera relancée dès sa fin.
                    Some(Status::Stopping) => session.request_restart(),
                    Some(Status::Running) => session.rearm_probe(),
                    _ => {
                        let done = session.wait.is_none()
                            && session.kind == Kind::Task
                            && session.has_succeeded();
                        if !done {
                            session.wait = Some(Wait::On(Vec::new()));
                        }
                        queue.push(dependency);
                    }
                }
            }
        }
        self.advance(&involved)
    }

    /// Démarre les sessions en attente dont les dépendances sont satisfaites et met à
    /// jour la raison des autres. Un échec de lancement est journalisé et reste visible
    /// dans l'état de la session ; seul le premier parmi `reported` est renvoyé.
    fn advance(&mut self, reported: &HashSet<usize>) -> Result<()> {
        if self.sessions.iter().all(|s| s.wait.is_none()) {
            return Ok(());
        }
        let mut first_error = None;
        // Les dépendances d'abord : une chaîne avance ainsi en un seul passage.
        for index in self.dependency_order() {
            if matches!(
                self.sessions[index].wait,
                None | Some(Wait::LaunchFailed(_))
            ) {
                continue;
            }
            let (missing, failure) = self.unmet(index);
            let session = &mut self.sessions[index];
            if let Some((dependency, cause)) = failure {
                session.wait = Some(Wait::Blocked { dependency, cause });
            } else if !missing.is_empty() {
                session.wait = Some(Wait::On(missing));
            } else if let Err(err) = session.start() {
                eprintln!("{} : lancement impossible : {err}", session.name);
                session.wait = Some(Wait::LaunchFailed(err.to_string()));
                if reported.contains(&index) {
                    first_error.get_or_insert_with(|| anyhow!("{} : {err}", session.name));
                }
            }
        }
        first_error.map_or(Ok(()), Err)
    }

    /// Ce qui retient une session : les dépendances encore attendues et la première en
    /// échec. Une tâche déjà réussie ne dispense pas de ses propres dépendances.
    fn unmet(&self, index: usize) -> (Vec<String>, Option<(String, Cause)>) {
        let mut missing = Vec::new();
        let mut failure = None;
        // En largeur : les dépendances directes sont citées avant les indirectes.
        let mut queue = VecDeque::from(self.dependencies(index));
        let mut visited = HashSet::new();
        while let Some(dependency) = queue.pop_front() {
            if !visited.insert(dependency) {
                continue;
            }
            let session = &self.sessions[dependency];
            match self.verdict(dependency) {
                Verdict::Satisfied => {
                    if session.kind == Kind::Task {
                        queue.extend(self.dependencies(dependency));
                    }
                }
                Verdict::Pending => missing.push(session.name.clone()),
                Verdict::Failed(cause) => {
                    failure.get_or_insert_with(|| (session.name.clone(), cause));
                }
            }
        }
        (missing, failure)
    }

    /// Où en est une session vue par celles qui en dépendent.
    fn verdict(&self, index: usize) -> Verdict {
        let session = &self.sessions[index];
        match &session.wait {
            Some(Wait::On(_)) => return Verdict::Pending,
            Some(_) => return Verdict::Failed(Cause::Blocked),
            None => {}
        }
        if session.restart_pending() {
            return Verdict::Pending;
        }
        match (session.status(), session.kind) {
            (None, _) => Verdict::Failed(Cause::NotStarted),
            (Some(Status::Stopping), _) | (Some(Status::Running), Kind::Task) => Verdict::Pending,
            (Some(Status::Running), Kind::Service) => match session.readiness() {
                Readiness::Ready => Verdict::Satisfied,
                Readiness::Probing => Verdict::Pending,
                Readiness::TimedOut => Verdict::Failed(Cause::NotReady),
            },
            (Some(Status::Exited(0)), Kind::Task) if session.has_succeeded() => Verdict::Satisfied,
            (Some(Status::Exited(0)), Kind::Task) if session.was_stopped() => {
                Verdict::Failed(Cause::NotStarted)
            }
            (Some(Status::Exited(0)), Kind::Task) => Verdict::Failed(Cause::Changed),
            (Some(Status::Exited(code)), _) => Verdict::Failed(Cause::Exited(code)),
        }
    }

    /// Indices des dépendances directes d'une session.
    fn dependencies(&self, index: usize) -> Vec<usize> {
        self.sessions[index]
            .depends_on
            .iter()
            .filter_map(|name| self.index_of(name))
            .collect()
    }

    /// Toutes les sessions, chacune après ses dépendances (la config exclut les cycles).
    fn dependency_order(&self) -> Vec<usize> {
        fn visit(manager: &Manager, index: usize, seen: &mut [bool], order: &mut Vec<usize>) {
            if std::mem::replace(&mut seen[index], true) {
                return;
            }
            for dependency in manager.dependencies(index) {
                visit(manager, dependency, seen, order);
            }
            order.push(index);
        }

        let mut seen = vec![false; self.sessions.len()];
        let mut order = Vec::with_capacity(self.sessions.len());
        for index in 0..self.sessions.len() {
            visit(self, index, &mut seen, &mut order);
        }
        order
    }
}

#[cfg(test)]
mod tests {
    use std::path::PathBuf;
    use std::thread;
    use std::time::{Duration, Instant};

    use super::*;
    use crate::config::Config;
    use crate::daemon::{Command, State, Target};

    /// Dossier jetable où les commandes des sessions déposent leurs fichiers témoins.
    struct Fixture {
        dir: PathBuf,
        manager: Manager,
    }

    impl Fixture {
        fn new(name: &str, sessions: &str) -> Self {
            let dir =
                std::env::temp_dir().join(format!("lazyshell-test-{}-{name}", std::process::id()));
            let _ = std::fs::remove_dir_all(&dir);
            std::fs::create_dir_all(&dir).unwrap();
            let manager = Manager::new(&config(sessions), &dir);
            Fixture { dir, manager }
        }

        fn start(&mut self, name: &str) {
            self.send(Command::Start(Target::Session(name.into())));
        }

        fn stop(&mut self, name: &str) {
            self.send(Command::Stop(Target::Session(name.into())));
        }

        fn restart(&mut self, name: &str) {
            self.send(Command::Restart(Target::Session(name.into())));
        }

        fn send(&mut self, command: Command) {
            self.manager.command(command).unwrap();
        }

        fn state(&self, name: &str) -> State {
            self.info(name).state
        }

        fn info(&self, name: &str) -> crate::daemon::SessionInfo {
            self.manager
                .infos()
                .into_iter()
                .find(|i| i.name == name)
                .unwrap()
        }

        fn run_id(&self, name: &str) -> Option<u64> {
            self.info(name).run_id
        }

        fn touch(&self, file: &str) {
            std::fs::write(self.dir.join(file), "").unwrap();
        }

        fn lines(&self, file: &str) -> usize {
            std::fs::read_to_string(self.dir.join(file)).map_or(0, |text| text.lines().count())
        }

        /// Fait tourner l'ordonnanceur jusqu'à ce que la session atteigne l'état voulu.
        fn wait_for(&mut self, name: &str, expected: State) {
            let deadline = Instant::now() + Duration::from_secs(10);
            while self.state(name) != expected {
                assert!(
                    Instant::now() < deadline,
                    "{name} : {:?} au lieu de {expected:?}",
                    self.info(name)
                );
                thread::sleep(Duration::from_millis(10));
                self.manager.tick();
            }
        }

        fn tick_for(&mut self, duration: Duration) {
            let deadline = Instant::now() + duration;
            while Instant::now() < deadline {
                thread::sleep(Duration::from_millis(10));
                self.manager.tick();
            }
        }

        fn wait_exit(&mut self, name: &str) {
            let deadline = Instant::now() + Duration::from_secs(5);
            while !matches!(self.state(name), State::Exited(_)) {
                assert!(Instant::now() < deadline, "{:?}", self.info(name));
                self.tick_for(Duration::from_millis(10));
            }
        }

        fn blocked_on(&self, name: &str) -> (String, Cause) {
            match self.info(name).wait {
                Some(Wait::Blocked { dependency, cause }) => (dependency, cause),
                other => panic!("{name} n'est pas bloquée : {other:?}"),
            }
        }
    }

    impl Drop for Fixture {
        fn drop(&mut self) {
            self.manager.shutdown();
            let _ = std::fs::remove_dir_all(&self.dir);
        }
    }

    fn config(sessions: &str) -> Config {
        serde_yaml::from_str(&format!("shell: /bin/sh\nsessions:\n{sessions}")).unwrap()
    }

    /// Tâche qui se termine quand le fichier `go` apparaît.
    const GATED_TASK: &str = "
  - name: install
    type: task
    command: echo run >> runs; while [ ! -f go ]; do sleep 0.05; done
  - name: app
    command: sleep 30
    depends_on: [install]";

    #[test]
    fn dependent_waits_for_a_task_to_finish() {
        let mut f = Fixture::new("task", GATED_TASK);
        f.start("app");
        assert_eq!(f.state("install"), State::Running);
        assert_eq!(f.state("app"), State::Waiting);
        assert_eq!(f.info("app").wait, Some(Wait::On(vec!["install".into()])));
        assert!(f.manager.has_active());
        f.touch("go");
        f.wait_for("app", State::Running);
        assert_eq!(f.state("install"), State::Exited(0));
    }

    #[test]
    fn autostart_pulls_dependencies() {
        let mut f = Fixture::new(
            "autostart",
            "
  - {name: install, type: task, command: 'true'}
  - {name: app, command: sleep 30, autostart: true, depends_on: [install]}",
        );
        f.wait_for("app", State::Running);
        assert_eq!(f.state("install"), State::Exited(0));
    }

    #[test]
    fn service_without_probe_does_not_hold_the_dependent() {
        let mut f = Fixture::new(
            "service",
            "
  - {name: db, command: sleep 30}
  - {name: app, command: sleep 30, depends_on: [db]}",
        );
        f.start("app");
        assert_eq!(f.state("db"), State::Running);
        assert_eq!(f.state("app"), State::Running);
    }

    #[test]
    fn dependent_waits_for_the_probe() {
        let mut f = Fixture::new(
            "probe",
            "
  - {name: db, command: sleep 30, ready: {command: test -f up}}
  - {name: app, command: sleep 30, depends_on: [db]}",
        );
        f.start("app");
        f.tick_for(Duration::from_millis(200));
        assert_eq!(f.state("db"), State::Running);
        assert_eq!(f.state("app"), State::Waiting);
        f.touch("up");
        f.wait_for("app", State::Running);
    }

    #[test]
    fn expired_probe_blocks_then_is_retried() {
        let mut f = Fixture::new(
            "timeout",
            "
  - {name: db, command: sleep 30, ready: {command: test -f up, timeout: 1}}
  - {name: app, command: sleep 30, depends_on: [db]}",
        );
        f.start("app");
        let db_run = f.run_id("db");
        f.wait_for("app", State::Blocked);
        assert_eq!(f.blocked_on("app"), ("db".into(), Cause::NotReady));
        assert_eq!(f.state("db"), State::Running);
        // Redemander l'application rejoue la sonde sans relancer le service.
        f.touch("up");
        f.start("app");
        f.wait_for("app", State::Running);
        assert_eq!(f.run_id("db"), db_run);
    }

    #[test]
    fn hung_probe_is_killed_at_the_deadline() {
        let mut f = Fixture::new(
            "hung",
            "
  - {name: db, command: sleep 30, ready: {command: 'sleep 20; touch late', timeout: 1}}
  - {name: app, command: sleep 30, depends_on: [db]}",
        );
        f.start("app");
        let started = Instant::now();
        f.wait_for("app", State::Blocked);
        assert!(started.elapsed() < Duration::from_secs(5));
    }

    #[test]
    fn failed_task_blocks_until_it_succeeds() {
        let mut f = Fixture::new(
            "failed",
            "
  - {name: install, type: task, command: test -f ok}
  - {name: app, command: sleep 30, depends_on: [install]}
  - {name: front, command: sleep 30, depends_on: [app]}",
        );
        f.start("front");
        f.wait_for("front", State::Blocked);
        assert_eq!(f.blocked_on("app"), ("install".into(), Cause::Exited(1)));
        assert_eq!(f.blocked_on("front"), ("app".into(), Cause::Blocked));
        assert!(!f.manager.has_active());
        // Pas de nouvel essai automatique.
        let run = f.run_id("install");
        f.tick_for(Duration::from_millis(200));
        assert_eq!(f.run_id("install"), run);
        // Relancer la tâche débloque toute la chaîne.
        f.touch("ok");
        f.start("install");
        f.wait_for("front", State::Running);
        assert_eq!(f.state("app"), State::Running);
    }

    #[test]
    fn asking_again_reruns_a_failed_task() {
        let mut f = Fixture::new(
            "again",
            "
  - {name: install, type: task, command: test -f ok}
  - {name: app, command: sleep 30, depends_on: [install]}",
        );
        f.start("app");
        f.wait_for("app", State::Blocked);
        f.touch("ok");
        f.start("app");
        f.wait_for("app", State::Running);
    }

    #[test]
    fn stopping_a_waiting_session_cancels_it() {
        let mut f = Fixture::new("cancel", GATED_TASK);
        f.start("app");
        f.stop("app");
        assert_eq!(f.state("app"), State::NotStarted);
        f.touch("go");
        f.wait_for("install", State::Exited(0));
        f.tick_for(Duration::from_millis(100));
        assert_eq!(f.state("app"), State::NotStarted);
    }

    #[test]
    fn stopping_a_dependency_blocks_the_waiting_session() {
        let mut f = Fixture::new("stopdep", GATED_TASK);
        f.start("app");
        f.stop("install");
        f.wait_for("app", State::Blocked);
        assert!(matches!(f.blocked_on("app").1, Cause::Exited(_)));
    }

    #[test]
    fn successful_task_runs_once_unless_asked() {
        let mut f = Fixture::new(
            "once",
            "
  - {name: install, type: task, command: echo run >> runs}
  - {name: web, command: sleep 30, depends_on: [install]}
  - {name: admin, command: sleep 30, depends_on: [install]}",
        );
        f.start("web");
        f.start("admin");
        f.wait_for("web", State::Running);
        f.wait_for("admin", State::Running);
        f.restart("web");
        f.wait_for("web", State::Running);
        f.stop("admin");
        f.wait_exit("admin");
        f.start("admin");
        assert_eq!(f.state("admin"), State::Running);
        assert_eq!(f.lines("runs"), 1);
        f.start("install");
        f.wait_for("install", State::Exited(0));
        assert_eq!(f.lines("runs"), 2);
    }

    #[test]
    fn restarting_a_dependency_leaves_dependents_alone() {
        let mut f = Fixture::new(
            "nopropagation",
            "
  - {name: db, command: sleep 30}
  - {name: app, command: sleep 30, depends_on: [db]}",
        );
        f.start("app");
        let (db, app) = (f.run_id("db"), f.run_id("app"));
        f.restart("db");
        let deadline = Instant::now() + Duration::from_secs(10);
        while f.run_id("db") == db || f.state("db") != State::Running {
            assert!(Instant::now() < deadline);
            f.tick_for(Duration::from_millis(20));
        }
        assert_eq!(f.run_id("app"), app);
        assert_eq!(f.state("app"), State::Running);
        f.stop("db");
        f.wait_exit("db");
        assert_eq!(f.state("app"), State::Running);
    }

    #[test]
    fn group_restart_keeps_the_order() {
        let mut f = Fixture::new(
            "group",
            "
  - {name: app, group: g, command: sleep 30, depends_on: [db]}
  - {name: db, group: g, command: sleep 30, ready: {command: test -f up}}",
        );
        f.touch("up");
        f.send(Command::Start(Target::Group("g".into())));
        f.wait_for("app", State::Running);
        let app = f.run_id("app");
        std::fs::remove_file(f.dir.join("up")).unwrap();
        f.send(Command::Restart(Target::Group("g".into())));
        f.wait_for("app", State::Waiting);
        f.tick_for(Duration::from_millis(300));
        assert_eq!(f.state("app"), State::Waiting);
        f.touch("up");
        f.wait_for("app", State::Running);
        assert_ne!(f.run_id("app"), app);
    }

    #[test]
    fn stopping_cancels_a_pending_restart() {
        let mut f = Fixture::new("stoprestart", "  - {name: app, command: sleep 30}");
        f.start("app");
        let run = f.run_id("app");
        f.restart("app");
        f.stop("app");
        f.wait_exit("app");
        f.tick_for(Duration::from_millis(100));
        assert_eq!(f.run_id("app"), run);
    }

    #[test]
    fn reload_makes_a_changed_task_run_again() {
        let sessions = |marker: &str| {
            format!(
                "
  - {{name: install, type: task, command: echo {marker} >> runs}}
  - {{name: app, command: sleep 30, depends_on: [install]}}"
            )
        };
        let mut f = Fixture::new("reload", &sessions("a"));
        f.start("app");
        f.wait_for("app", State::Running);
        f.stop("app");
        f.wait_exit("app");
        let dir = f.dir.clone();
        f.manager.reload(&config(&sessions("b")), &dir).unwrap();
        f.start("app");
        f.wait_for("app", State::Running);
        assert_eq!(f.lines("runs"), 2);
    }

    #[test]
    fn reload_starts_the_new_dependency_of_a_waiting_session() {
        let mut f = Fixture::new("rewire", GATED_TASK);
        f.start("app");
        let dir = f.dir.clone();
        let rewired = format!(
            "{}
  - {{name: extra, type: task, command: 'true'}}",
            GATED_TASK.replace("[install]", "[install, extra]")
        );
        let report = f.manager.reload(&config(&rewired), &dir).unwrap();
        assert_eq!(report.changed, ["app"]);
        f.touch("go");
        f.wait_for("app", State::Running);
        assert_eq!(f.state("extra"), State::Exited(0));
    }

    #[test]
    fn unlaunchable_session_is_reported_and_stays_blocked() {
        // Un octet nul dans une variable empêche la création du processus.
        let mut f = Fixture::new(
            "launch",
            r#"
  - name: install
    type: task
    command: while [ ! -f go ]; do sleep 0.05; done
  - name: app
    command: sleep 30
    env: {BROKEN: "a\0b"}
    depends_on: [install]
  - name: front
    command: sleep 30
    depends_on: [app]"#,
        );
        f.start("front");
        f.touch("go");
        f.wait_for("app", State::Blocked);
        assert!(matches!(f.info("app").wait, Some(Wait::LaunchFailed(_))));
        f.wait_for("front", State::Blocked);
        // Redemandée directement, la session renvoie l'erreur à l'appelant.
        let error = f
            .manager
            .command(Command::Start(Target::Session("app".into())));
        assert!(error.unwrap_err().to_string().starts_with("app : "));
    }

    #[test]
    fn dependencies_of_a_succeeded_task_are_started_again() {
        let mut f = Fixture::new(
            "transitive",
            "
  - {name: db, command: sleep 30, ready: {command: test -f up}}
  - {name: migrate, type: task, command: echo run >> runs, depends_on: [db]}
  - {name: app, command: sleep 30, depends_on: [migrate]}",
        );
        f.touch("up");
        f.start("app");
        f.wait_for("app", State::Running);
        f.stop("app");
        f.stop("db");
        f.wait_exit("app");
        f.wait_exit("db");
        std::fs::remove_file(f.dir.join("up")).unwrap();
        f.start("app");
        assert_eq!(f.state("db"), State::Running);
        assert_eq!(f.info("app").wait, Some(Wait::On(vec!["db".into()])));
        f.touch("up");
        f.wait_for("app", State::Running);
        assert_eq!(f.lines("runs"), 1);
    }

    #[test]
    fn stopping_dependency_is_restarted_when_required() {
        let mut f = Fixture::new(
            "stoppingdep",
            r#"
  - name: db
    command: trap 'sleep 0.5; exit 0' TERM; echo up >> ups; while :; do sleep 0.05; done
  - name: app
    command: sleep 30
    depends_on: [db]"#,
        );
        f.start("db");
        let run = f.run_id("db");
        let deadline = Instant::now() + Duration::from_secs(5);
        while f.lines("ups") == 0 {
            assert!(Instant::now() < deadline);
            f.tick_for(Duration::from_millis(10));
        }
        f.stop("db");
        assert_eq!(f.state("db"), State::Stopping);
        f.start("app");
        assert_eq!(f.state("app"), State::Waiting);
        f.wait_for("app", State::Running);
        assert_eq!(f.state("db"), State::Running);
        assert_ne!(f.run_id("db"), run);
    }

    #[test]
    fn stopped_task_exiting_zero_has_not_succeeded() {
        let mut f = Fixture::new(
            "stoppedtask",
            r#"
  - name: install
    type: task
    command: trap 'exit 0' TERM; echo run >> runs; while [ ! -f go ]; do sleep 0.05; done
  - name: app
    command: sleep 30
    depends_on: [install]"#,
        );
        f.start("app");
        let deadline = Instant::now() + Duration::from_secs(5);
        while f.lines("runs") == 0 {
            assert!(Instant::now() < deadline);
            f.tick_for(Duration::from_millis(10));
        }
        f.stop("install");
        f.wait_for("app", State::Blocked);
        assert_eq!(f.state("install"), State::Exited(0));
        assert_eq!(f.blocked_on("app"), ("install".into(), Cause::NotStarted));
        // Redemandée, l'application rejoue la tâche interrompue.
        f.start("app");
        f.touch("go");
        f.wait_for("app", State::Running);
        assert_eq!(f.lines("runs"), 2);
    }

    #[test]
    fn starting_a_service_again_retries_its_expired_probe() {
        let mut f = Fixture::new(
            "rearm",
            "
  - {name: db, command: sleep 30, ready: {command: test -f up, timeout: 1}}
  - {name: app, command: sleep 30, depends_on: [db]}",
        );
        f.start("app");
        let run = f.run_id("db");
        f.wait_for("app", State::Blocked);
        f.touch("up");
        f.start("db");
        f.wait_for("app", State::Running);
        assert_eq!(f.run_id("db"), run);
    }

    /// Une session dont le processus ne peut pas être créé (octet nul dans une variable).
    const BROKEN: &str = r#"
  - name: install
    type: task
    command: while [ ! -f go ]; do sleep 0.05; done
  - name: broken
    command: sleep 30
    env: {BROKEN: "a\0b"}
    depends_on: [install]
  - name: other
    command: sleep 30"#;

    #[test]
    fn launch_errors_of_other_sessions_are_not_reported() {
        for command in [Command::Start, Command::Restart] {
            let mut f = Fixture::new("unrelated", BROKEN);
            f.start("broken");
            f.touch("go");
            // Sans `tick` : la demande suivante est la première à tenter de lancer `broken`.
            let deadline = Instant::now() + Duration::from_secs(5);
            while f.state("install") != State::Exited(0) {
                assert!(Instant::now() < deadline);
                thread::sleep(Duration::from_millis(10));
            }
            f.send(command(Target::Session("other".into())));
            assert!(matches!(f.info("broken").wait, Some(Wait::LaunchFailed(_))));
            assert_eq!(f.state("other"), State::Running);
        }
    }

    #[test]
    fn launch_error_of_a_dependency_is_reported() {
        let mut f = Fixture::new(
            "brokendep",
            r#"
  - name: db
    command: sleep 30
    env: {BROKEN: "a\0b"}
  - name: app
    command: sleep 30
    depends_on: [db]"#,
        );
        let error = f
            .manager
            .command(Command::Start(Target::Session("app".into())));
        assert!(error.unwrap_err().to_string().starts_with("db : "));
        assert_eq!(f.state("app"), State::Blocked);
    }

    #[test]
    fn launch_failure_neither_aborts_startup_nor_refuses_a_reload() {
        let broken = |name: &str| {
            format!(
                "
  - {{name: {name}, command: sleep 30, autostart: true, env: {{BROKEN: \"a\\0b\"}}}}"
            )
        };
        let first = format!(
            "
  - {{name: ok, command: sleep 30, autostart: true}}{}",
            broken("early")
        );
        let mut f = Fixture::new("startup", &first);
        assert_eq!(f.state("ok"), State::Running);
        assert_eq!(f.state("early"), State::Blocked);
        let dir = f.dir.clone();
        let second = format!("{first}{}", broken("late"));
        let report = f.manager.reload(&config(&second), &dir).unwrap();
        assert_eq!(report.added, ["late"]);
        assert_eq!(f.state("late"), State::Blocked);
        assert_eq!(f.state("ok"), State::Running);
    }
}
