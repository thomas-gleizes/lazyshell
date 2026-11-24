use std::path::{Path, PathBuf};
use std::time::{Duration, Instant};

use anyhow::{Context, Result};
use crossterm::event::{
    self, DisableMouseCapture, EnableMouseCapture, Event, KeyCode, KeyEvent, KeyEventKind,
    KeyModifiers, MouseButton, MouseEventKind,
};
use crossterm::execute;
use crossterm::terminal::SetTitle;
use ratatui::layout::{Constraint, Layout, Rect};
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Block, Borders, List, ListItem, ListState, Paragraph};
use ratatui::{DefaultTerminal, Frame};
use tui_term::vt100;
use tui_term::widget::{Cell as _, PseudoTerminal};

use crate::client::{Client, Stream, StreamEvent};
use crate::daemon::{Command, SessionInfo, State, Target};
use crate::{i18n, paths, search};

/// Hauteur du panneau de la commande (cadre compris).
const COMMAND_HEIGHT: u16 = 3;
const PAGE_SCROLL: usize = 10;
const WHEEL_SCROLL: usize = 3;
const SCROLLBACK_LINES: usize = 5000;
/// Défilement horizontal (colonnes par appui) quand le soft-wrap est désactivé.
const HSCROLL_STEP: usize = 8;
/// Largeur du terminal émulé quand le soft-wrap est désactivé : au-delà, les lignes se replient.
const NOWRAP_COLS: u16 = 200;
/// Évents de flux traités par tour de boucle, pour que l'interface reste réactive.
const EVENTS_PER_TICK: usize = 256;
/// Intervalle entre deux relevés des autres daemons (connexion + liste de leurs sessions).
const DAEMON_REFRESH: Duration = Duration::from_secs(2);
/// Nombre maximal de lignes du panneau des daemons avant qu'il ne défile.
const DAEMON_ROWS: usize = 5;

/// Sortie de la session affichée : flux du daemon, écran émulé et défilement local.
struct View {
    session: String,
    run_id: u64,
    stream: Stream,
    parser: vt100::Parser,
    /// Taille (lignes, colonnes) du terminal émulé et du PTY, plus large que le panneau sans wrap.
    size: (u16, u16),
    scroll: usize,
    /// Recherche en cours : texte et ligne (depuis la plus ancienne) du dernier résultat.
    query: Option<String>,
    found_at: Option<usize>,
}

#[derive(Clone, Copy)]
enum Jump {
    Newest,
    Older,
    Newer,
}

/// Onglet du panneau de droite.
#[derive(Clone, Copy, PartialEq, Eq)]
enum Tab {
    Output,
    Env,
    Usage,
}

impl Tab {
    const ALL: [Tab; 3] = [Tab::Output, Tab::Env, Tab::Usage];

    fn label(self) -> &'static str {
        match self {
            Tab::Output => i18n::tab_output(),
            Tab::Env => i18n::tab_env(),
            Tab::Usage => i18n::tab_usage(),
        }
    }

    fn next(self) -> Tab {
        match self {
            Tab::Output => Tab::Env,
            Tab::Env => Tab::Usage,
            Tab::Usage => Tab::Output,
        }
    }

    fn previous(self) -> Tab {
        match self {
            Tab::Output => Tab::Usage,
            Tab::Env => Tab::Output,
            Tab::Usage => Tab::Env,
        }
    }
}

/// Daemon lazyshell joignable : un par fichier de config.
struct DaemonEntry {
    config: PathBuf,
    socket: PathBuf,
    /// Libellé court (dossier de la config), allongé si deux daemons portent le même.
    label: String,
    /// Sessions en cours d'exécution dans ce daemon.
    running: usize,
}

/// Panneau de gauche qui reçoit les touches de navigation.
#[derive(Clone, Copy, PartialEq, Eq)]
enum Focus {
    Daemons,
    Sessions,
}

struct App {
    /// Fichier de config et socket du daemon affiché.
    config_file: PathBuf,
    socket: PathBuf,
    control: Client,
    /// Copie de l'état des sessions, rafraîchie à chaque tour de boucle.
    infos: Vec<SessionInfo>,
    selected: usize,
    view: Option<View>,
    tab: Tab,
    /// Daemons connus (dont l'affiché), rafraîchis toutes les `DAEMON_REFRESH`.
    daemons: Vec<DaemonEntry>,
    daemons_checked: Instant,
    /// Daemon surligné dans le panneau (Entrée pour basculer dessus).
    daemon_sel: usize,
    focus: Focus,
    daemons_area: Rect,
    daemons_state: ListState,
    /// Zones cliquables des onglets, mises à jour à chaque affichage.
    tab_areas: Vec<(Tab, Rect)>,
    /// Zone de la liste des sessions, état de défilement et session de chaque ligne (None = titre de groupe).
    list_area: Rect,
    list_state: ListState,
    list_rows: Vec<Option<usize>>,
    /// Variables de la session sélectionnée (onglet Env) et défilement depuis le haut.
    env: Vec<(String, String)>,
    env_scroll: usize,
    /// Soft-wrap de la sortie (`w`) ; désactivé, les lignes sont tronquées et défilent horizontalement.
    wrap: bool,
    /// Décalage horizontal (colonnes) de la sortie quand le wrap est désactivé.
    hscroll: usize,
    /// Saisie de recherche en cours (`/`).
    input: Option<String>,
    show_help: bool,
    message: Option<String>,
    quit: bool,
    shutdown_requested: bool,
    /// Plein écran (`f`) : la sortie occupe tout le terminal, la liste est masquée.
    fullscreen: bool,
    /// Intérieur du panneau de droite (sortie ou Env), mis à jour à chaque affichage.
    out_inner: Rect,
    /// Sélection à la souris dans le panneau, en coordonnées d'écran.
    selection: Option<Selection>,
    /// Texte actuellement sélectionné, relu dans le tampon à chaque affichage.
    selected_text: String,
    /// Dernier titre envoyé au terminal (évite de le renvoyer à chaque tour de boucle).
    title: String,
}

/// Sélection au glisser : `anchor` là où le bouton a été pressé, `cursor` la position courante.
#[derive(Clone, Copy)]
struct Selection {
    anchor: (u16, u16),
    cursor: (u16, u16),
    dragging: bool,
}

impl Selection {
    /// Bornes dans l'ordre de lecture (début, fin).
    fn ordered(&self) -> ((u16, u16), (u16, u16)) {
        let (a, c) = (self.anchor, self.cursor);
        if (a.1, a.0) <= (c.1, c.0) {
            (a, c)
        } else {
            (c, a)
        }
    }
}

pub fn run(config_file: &Path, socket: &Path) -> Result<()> {
    let mut control = Client::connect(socket)?;
    let mut app = App {
        infos: control.list()?,
        control,
        config_file: config_file.to_path_buf(),
        socket: socket.to_path_buf(),
        daemons: Vec::new(),
        daemons_checked: Instant::now(),
        daemon_sel: 0,
        focus: Focus::Sessions,
        daemons_area: Rect::default(),
        daemons_state: ListState::default(),
        selected: 0,
        view: None,
        tab: Tab::Output,
        tab_areas: Vec::new(),
        list_area: Rect::default(),
        list_state: ListState::default(),
        list_rows: Vec::new(),
        env: Vec::new(),
        env_scroll: 0,
        wrap: true,
        hscroll: 0,
        input: None,
        show_help: false,
        message: None,
        quit: false,
        shutdown_requested: false,
        fullscreen: false,
        out_inner: Rect::default(),
        selection: None,
        selected_text: String::new(),
        title: String::new(),
    };
    let mut terminal = ratatui::init();
    execute!(std::io::stdout(), EnableMouseCapture)?;
    let result = app.event_loop(&mut terminal);
    // Titre vide : le terminal reprend son titre par défaut.
    let _ = execute!(std::io::stdout(), DisableMouseCapture, SetTitle(""));
    ratatui::restore();
    result
}

/// En plein écran, la liste n'a plus de place (zone vide) et la sortie prend tout.
/// Retourne (liste, sortie, commande, pied) ; la commande occupe toute la largeur en bas.
fn layout(area: Rect, fullscreen: bool) -> (Rect, Rect, Rect, Rect) {
    let [main, command, footer] = Layout::vertical([
        Constraint::Min(1),
        Constraint::Length(COMMAND_HEIGHT),
        Constraint::Length(1),
    ])
    .areas(area);
    if fullscreen {
        return (Rect::default(), main, command, footer);
    }
    let [left, right] =
        Layout::horizontal([Constraint::Length(32), Constraint::Min(20)]).areas(main);
    (left, right, command, footer)
}

/// Taille (lignes, colonnes) utile du panneau de sortie pour un terminal donné.
fn output_size(area: Rect, fullscreen: bool) -> (u16, u16) {
    let (_, right, _, _) = layout(area, fullscreen);
    let inner = Block::new().borders(Borders::ALL).inner(right);
    (inner.height.max(1), inner.width.max(1))
}

impl App {
    fn current(&self) -> &SessionInfo {
        &self.infos[self.selected]
    }

    fn event_loop(&mut self, terminal: &mut DefaultTerminal) -> Result<()> {
        while !self.quit {
            self.infos = self.control.list().context("connexion au daemon perdue")?;
            self.selected = self.selected.min(self.infos.len() - 1);
            self.refresh_daemons();
            let size = terminal.size()?;
            self.sync_view(output_size(
                Rect::new(0, 0, size.width, size.height),
                self.fullscreen,
            ));
            self.sync_env();
            self.update_title();
            terminal.draw(|frame| self.draw(frame))?;

            if self.shutdown_requested {
                self.control.shutdown()?;
                break;
            }
            if event::poll(Duration::from_millis(50))? {
                match event::read()? {
                    Event::Key(key) if key.kind == KeyEventKind::Press => self.on_key(key),
                    Event::Mouse(mouse) => match mouse.kind {
                        MouseEventKind::ScrollUp => self.wheel(mouse.column, mouse.row, true),
                        MouseEventKind::ScrollDown => self.wheel(mouse.column, mouse.row, false),
                        MouseEventKind::Down(MouseButton::Left) => {
                            self.click(mouse.column, mouse.row)
                        }
                        MouseEventKind::Drag(MouseButton::Left) => {
                            self.drag(mouse.column, mouse.row)
                        }
                        MouseEventKind::Up(MouseButton::Left) => self.release(),
                        _ => {}
                    },
                    _ => {}
                }
            }
        }
        Ok(())
    }

    /// Met à jour le titre de l'onglet du terminal (séquence OSC 2) : « ● lazyshell · projet · session » (l'icône est l'état de la session).
    /// Le projet est omis tant que les daemons ne sont pas relevés.
    fn update_title(&mut self) {
        let mut parts = vec!["lazyshell".to_string()];
        if let Some(entry) = self.daemons.iter().find(|d| d.socket == self.socket) {
            parts.push(entry.label.clone());
        }
        if let Some(info) = self.infos.get(self.selected) {
            parts.push(info.name.clone());
        }
        let mut title = parts.join(" · ");
        if let Some(info) = self.infos.get(self.selected) {
            // Pas de couleur dans un titre : l'échec a son propre symbole (✖) plutôt qu'un ● rouge.
            let icon = match info.state {
                State::NotStarted | State::Exited(0) => '○',
                State::Running => '●',
                State::Stopping => '◌',
                State::Exited(_) => '✖',
            };
            title.insert_str(0, &format!("{icon} "));
        }
        if title != self.title {
            let _ = execute!(std::io::stdout(), SetTitle(title.as_str()));
            self.title = title;
        }
    }

    /// Relève les daemons joignables : celui affiché (état déjà connu) et les autres, auxquels
    /// on se connecte brièvement. Les sockets orphelines et les protocoles incompatibles sont ignorés.
    fn refresh_daemons(&mut self) {
        if !self.daemons.is_empty() && self.daemons_checked.elapsed() < DAEMON_REFRESH {
            return;
        }
        self.daemons_checked = Instant::now();
        let highlighted = self.daemons.get(self.daemon_sel).map(|d| d.socket.clone());
        let active = |infos: &[SessionInfo]| {
            infos
                .iter()
                .filter(|info| matches!(info.state, State::Running | State::Stopping))
                .count()
        };
        let mut entries = Vec::new();
        for (config, socket) in paths::known_daemons().unwrap_or_default() {
            let running = if socket == self.socket {
                active(&self.infos)
            } else {
                match Client::connect(&socket).and_then(|mut client| client.list()) {
                    Ok(infos) => active(&infos),
                    Err(_) => continue,
                }
            };
            entries.push(DaemonEntry {
                config,
                socket,
                label: String::new(),
                running,
            });
        }
        // Daemon sans fichier de config enregistré : il reste affiché, c'est celui où l'on est.
        if !entries.iter().any(|entry| entry.socket == self.socket) {
            entries.push(DaemonEntry {
                config: self.config_file.clone(),
                socket: self.socket.clone(),
                label: String::new(),
                running: active(&self.infos),
            });
            entries.sort_by(|a, b| a.config.cmp(&b.config));
        }
        let labels: Vec<String> = entries
            .iter()
            .map(|entry| daemon_label(&entry.config, 1))
            .collect();
        for (entry, label) in entries.iter_mut().zip(&labels) {
            entry.label = if labels.iter().filter(|other| *other == label).count() > 1 {
                daemon_label(&entry.config, 2)
            } else {
                label.clone()
            };
        }
        let keep = highlighted.unwrap_or_else(|| self.socket.clone());
        self.daemon_sel = entries
            .iter()
            .position(|entry| entry.socket == keep)
            .unwrap_or(0);
        self.daemons = entries;
    }

    /// Bascule la TUI sur un autre daemon : nouvelle connexion de contrôle, sélection et
    /// vue remises à zéro. Sans effet (et message d'erreur) si le daemon ne répond plus.
    fn switch_daemon(&mut self, index: usize) {
        let Some(entry) = self.daemons.get(index) else {
            return;
        };
        if entry.socket == self.socket {
            return;
        }
        let (config, socket, label) = (
            entry.config.clone(),
            entry.socket.clone(),
            entry.label.clone(),
        );
        let connected = Client::connect(&socket).and_then(|mut client| {
            let infos = client.list()?;
            Ok((client, infos))
        });
        match connected {
            Ok((client, infos)) if !infos.is_empty() => {
                self.control = client;
                self.infos = infos;
                self.config_file = config;
                self.socket = socket;
                self.selected = 0;
                self.view = None;
                self.env.clear();
                self.env_scroll = 0;
                self.hscroll = 0;
                self.selection = None;
                self.focus = Focus::Sessions;
                self.message = Some(i18n::daemon_switched(&label));
                self.daemons_checked = Instant::now() - DAEMON_REFRESH;
            }
            Ok(_) => self.message = Some(i18n::daemon_empty(&label)),
            Err(err) => self.message = Some(format!("{label} : {err}")),
        }
    }

    /// Aligne la vue sur la session sélectionnée et son exécution courante, applique
    /// la taille du panneau et absorbe le flux reçu.
    fn sync_view(&mut self, pane: (u16, u16)) {
        let size = self.emulated_size(pane);
        let info = self.current().clone();
        let stale = match &self.view {
            Some(view) => view.session != info.name || Some(view.run_id) != info.run_id,
            None => true,
        };
        if stale {
            self.view = None;
            if info.run_id.is_some() {
                self.open_view(&info.name, size);
            }
        }

        let Some(view) = &mut self.view else { return };
        if view.size != size {
            view.parser.screen_mut().set_size(size.0, size.1);
            view.size = size;
            let _ = self.control.resize(&view.session, size.0, size.1);
        }
        for _ in 0..EVENTS_PER_TICK {
            match view.stream.try_recv() {
                Some(StreamEvent::Data(data)) => view.parser.process(&data),
                Some(StreamEvent::End) | None => break,
            }
        }
    }

    /// Rafraîchit les variables de la session sélectionnée quand l'onglet Env est affiché.
    fn sync_env(&mut self) {
        if self.tab != Tab::Env {
            return;
        }
        let name = self.current().name.clone();
        match self.control.env(&name) {
            Ok(vars) => self.env = vars,
            Err(err) => {
                self.env.clear();
                self.message = Some(err.to_string());
            }
        }
    }

    fn set_tab(&mut self, tab: Tab) {
        if self.tab != tab {
            self.tab = tab;
            self.env_scroll = 0;
        }
    }

    /// Molette : sur la liste elle déplace la sélection, ailleurs elle défile la sortie.
    fn wheel(&mut self, column: u16, row: u16, up: bool) {
        self.selection = None;
        if self.daemons_area.contains((column, row).into()) {
            let last = self.daemons.len().saturating_sub(1);
            self.daemon_sel = if up {
                self.daemon_sel.saturating_sub(1)
            } else {
                (self.daemon_sel + 1).min(last)
            };
            self.focus = Focus::Daemons;
        } else if self.list_area.contains((column, row).into()) {
            let last = self.infos.len() - 1;
            self.selected = if up {
                self.selected.saturating_sub(1)
            } else {
                (self.selected + 1).min(last)
            };
        } else {
            let step = WHEEL_SCROLL as isize;
            self.scroll_by(if up { step } else { -step });
        }
    }

    fn click(&mut self, column: u16, row: u16) {
        let pos = (column, row).into();
        self.selection = None;
        if self.out_inner.contains(pos) {
            self.selection = Some(Selection {
                anchor: (column, row),
                cursor: (column, row),
                dragging: true,
            });
            return;
        }
        if self.daemons_area.contains(pos) {
            let inner = Block::new().borders(Borders::ALL).inner(self.daemons_area);
            self.focus = Focus::Daemons;
            if inner.contains(pos) {
                let index = self.daemons_state.offset() + (row - inner.y) as usize;
                if index < self.daemons.len() {
                    self.daemon_sel = index;
                    self.switch_daemon(index);
                }
            }
            return;
        }
        if self.list_area.contains(pos) {
            let inner = Block::new().borders(Borders::ALL).inner(self.list_area);
            self.focus = Focus::Sessions;
            if inner.contains(pos) {
                let index = self.list_state.offset() + (row - inner.y) as usize;
                if let Some(&Some(session)) = self.list_rows.get(index) {
                    self.selected = session;
                }
            }
            return;
        }
        let hit = self
            .tab_areas
            .iter()
            .find(|(_, area)| area.contains((column, row).into()));
        if let Some(&(tab, _)) = hit {
            self.set_tab(tab);
        }
    }

    /// Étend la sélection en cours, la position est ramenée dans le panneau.
    fn drag(&mut self, column: u16, row: u16) {
        let inner = self.out_inner;
        if inner.width == 0 || inner.height == 0 {
            return;
        }
        let x = column.clamp(inner.x, inner.right() - 1);
        let y = row.clamp(inner.y, inner.bottom() - 1);
        if let Some(sel) = self.selection.as_mut().filter(|sel| sel.dragging) {
            sel.cursor = (x, y);
        }
    }

    /// Relâchement : un simple clic n'a rien sélectionné, un glisser copie le texte.
    fn release(&mut self) {
        let Some(sel) = &mut self.selection else {
            return;
        };
        sel.dragging = false;
        if sel.anchor == sel.cursor {
            self.selection = None;
            return;
        }
        if !self.selected_text.is_empty() {
            copy_to_clipboard(&self.selected_text);
            self.message = Some(i18n::copied(self.selected_text.chars().count()));
        }
    }

    /// Surligne la sélection et en relit le texte dans le tampon déjà dessiné.
    fn apply_selection(&mut self, frame: &mut Frame) {
        self.selected_text.clear();
        let Some(sel) = self.selection else { return };
        let inner = self.out_inner.intersection(frame.area());
        if inner.width == 0 || inner.height == 0 {
            return;
        }
        let ((sx, sy), (ex, ey)) = sel.ordered();
        let buf = frame.buffer_mut();
        let mut lines = Vec::new();
        for y in sy.max(inner.y)..=ey.min(inner.bottom() - 1) {
            let from = if y == sy { sx } else { inner.x };
            let to = if y == ey { ex } else { inner.right() - 1 };
            let mut line = String::new();
            for x in from.max(inner.x)..=to.min(inner.right() - 1) {
                let cell = &mut buf[(x, y)];
                line.push_str(cell.symbol());
                cell.set_style(Style::new().add_modifier(Modifier::REVERSED));
            }
            lines.push(line.trim_end().to_string());
        }
        self.selected_text = lines.join("\n").trim_end().to_string();
    }

    fn open_view(&mut self, session: &str, size: (u16, u16)) {
        // Le PTY prend la taille du panneau avant le rejeu, pour un affichage cohérent.
        let _ = self.control.resize(session, size.0, size.1);
        match Client::subscribe(&self.socket, session) {
            Ok(stream) => {
                self.view = Some(View {
                    session: session.to_string(),
                    run_id: stream.run_id,
                    stream,
                    parser: vt100::Parser::new(size.0, size.1, SCROLLBACK_LINES),
                    size,
                    scroll: 0,
                    query: None,
                    found_at: None,
                });
            }
            Err(err) => self.message = Some(format!("{session} : {err}")),
        }
    }

    fn on_key(&mut self, key: KeyEvent) {
        self.message = None;
        if self.show_help {
            self.show_help = false;
            return;
        }
        if self.input.is_some() {
            self.on_search_key(key);
            return;
        }
        let last = self.infos.len() - 1;
        let name = self.current().name.clone();
        let group = self.current().group.clone();
        let session = || Target::Session(name.clone());
        let group = || Target::Group(group.clone());
        match key.code {
            KeyCode::Char('q') => self.quit = true,
            KeyCode::Char('c') if key.modifiers.contains(KeyModifiers::CONTROL) => self.quit = true,
            KeyCode::Char('Q') => {
                self.message = Some(i18n::stopping_all().into());
                self.shutdown_requested = true;
            }
            KeyCode::Char('L') => match self.control.reload() {
                Ok(report) => self.message = Some(i18n::config_reloaded(&report.summary())),
                Err(err) => self.message = Some(i18n::reload_refused(&err)),
            },
            KeyCode::Tab => self.set_tab(self.tab.next()),
            KeyCode::BackTab => self.set_tab(self.tab.previous()),
            KeyCode::Char('/') => self.input = Some(String::new()),
            KeyCode::Char('n') => self.jump(Jump::Older),
            KeyCode::Char('N') => self.jump(Jump::Newer),
            KeyCode::Esc => {
                let searching = self.view.as_ref().is_some_and(|view| view.query.is_some());
                if let Some(view) = &mut self.view {
                    view.query = None;
                    view.found_at = None;
                }
                // Sans recherche à effacer, Échap quitte le plein écran.
                if self.fullscreen && !searching {
                    self.toggle_fullscreen();
                }
            }
            KeyCode::Char('?') => self.show_help = true,
            KeyCode::Char('w') => self.toggle_wrap(),
            KeyCode::Char('f') => self.toggle_fullscreen(),
            KeyCode::Left if !self.wrap => self.hscroll = self.hscroll.saturating_sub(HSCROLL_STEP),
            KeyCode::Right if !self.wrap => self.hscroll += HSCROLL_STEP,
            KeyCode::Char('1') => self.focus = Focus::Daemons,
            KeyCode::Char('2') => self.focus = Focus::Sessions,
            KeyCode::Enter if self.focus == Focus::Daemons => self.switch_daemon(self.daemon_sel),
            KeyCode::Char('j') | KeyCode::Down if self.focus == Focus::Daemons => {
                self.daemon_sel = (self.daemon_sel + 1).min(self.daemons.len().saturating_sub(1))
            }
            KeyCode::Char('k') | KeyCode::Up if self.focus == Focus::Daemons => {
                self.daemon_sel = self.daemon_sel.saturating_sub(1)
            }
            KeyCode::Char('j') | KeyCode::Down => self.selected = (self.selected + 1).min(last),
            KeyCode::Char('k') | KeyCode::Up => self.selected = self.selected.saturating_sub(1),
            KeyCode::Char('s') => self.send(Command::Start(session())),
            KeyCode::Char('x') => self.send(Command::Stop(session())),
            KeyCode::Char('r') => self.send(Command::Restart(session())),
            KeyCode::Char('S') => self.send(Command::Start(group())),
            KeyCode::Char('X') => self.send(Command::Stop(group())),
            KeyCode::Char('R') => self.send(Command::Restart(group())),
            KeyCode::PageUp => self.scroll_by(PAGE_SCROLL as isize),
            KeyCode::PageDown => self.scroll_by(-(PAGE_SCROLL as isize)),
            KeyCode::Char('g') => self.scroll_by(isize::MAX / 2),
            KeyCode::Char('G') => self.scroll_by(isize::MIN / 2),
            _ => {}
        }
    }

    fn on_search_key(&mut self, key: KeyEvent) {
        let Some(input) = &mut self.input else { return };
        match key.code {
            KeyCode::Esc => self.input = None,
            KeyCode::Enter => {
                let query = std::mem::take(input);
                self.input = None;
                if let Some(view) = &mut self.view {
                    view.query = (!query.is_empty()).then_some(query);
                    view.found_at = None;
                }
                self.jump(Jump::Newest);
            }
            KeyCode::Backspace => {
                input.pop();
            }
            KeyCode::Char(c) if !key.modifiers.contains(KeyModifiers::CONTROL) => input.push(c),
            _ => {}
        }
    }

    /// Va à l'occurrence suivante de la recherche : la plus récente, ou la plus
    /// ancienne / plus récente que la précédente.
    fn jump(&mut self, jump: Jump) {
        let Some(view) = &mut self.view else { return };
        let Some(query) = view.query.clone() else {
            if !matches!(jump, Jump::Newest) {
                self.message = Some(i18n::no_search().into());
            }
            return;
        };
        let lines = search::history_lines(&mut view.parser);
        let found = search::matching_lines(&lines, &query);
        let target = match (jump, view.found_at) {
            (Jump::Older, Some(at)) => found.iter().rev().find(|&&line| line < at),
            (Jump::Newer, Some(at)) => found.iter().find(|&&line| line > at),
            (Jump::Newest, _) | (_, None) => found.last(),
        };
        match target {
            Some(&line) => {
                view.found_at = Some(line);
                // Le défilement se compte depuis le bas : on place l'occurrence au milieu.
                let from_bottom = lines.len() - 1 - line;
                view.scroll = from_bottom.saturating_sub(view.size.0 as usize / 2);
            }
            None if found.is_empty() => self.message = Some(i18n::no_match(&query)),
            None => self.message = Some(i18n::no_other_match().into()),
        }
    }

    /// Taille du terminal émulé pour un panneau donné : sa largeur avec wrap, fixe sans.
    fn emulated_size(&self, pane: (u16, u16)) -> (u16, u16) {
        if self.wrap {
            pane
        } else {
            (pane.0, NOWRAP_COLS.max(pane.1))
        }
    }

    /// Bascule le soft-wrap. La vue est rouverte : le rejeu de l'historique se fait ainsi
    /// à la nouvelle largeur plutôt que sur un écran déjà replié.
    fn toggle_wrap(&mut self) {
        self.wrap = !self.wrap;
        self.hscroll = 0;
        self.view = None;
        self.message = Some(i18n::wrap_state(self.wrap).into());
    }

    /// Bascule le plein écran. Comme pour le wrap, la vue est rouverte pour rejouer
    /// l'historique à la nouvelle largeur.
    fn toggle_fullscreen(&mut self) {
        self.fullscreen = !self.fullscreen;
        self.view = None;
        self.message = Some(i18n::fullscreen_state(self.fullscreen).into());
    }

    /// Défile vers le passé (`delta` > 0, sortie) ou vers le haut (onglet Env) ; négatif en sens inverse.
    fn scroll_by(&mut self, delta: isize) {
        match self.tab {
            Tab::Output => {
                if let Some(view) = &mut self.view {
                    view.scroll = view.scroll.saturating_add_signed(delta);
                }
            }
            Tab::Env => self.env_scroll = self.env_scroll.saturating_add_signed(-delta),
            Tab::Usage => {}
        }
    }

    fn send(&mut self, command: Command) {
        if let Err(err) = self.control.command(command) {
            self.message = Some(err.to_string());
        }
    }

    fn draw(&mut self, frame: &mut Frame) {
        let (left, right, command, footer) = layout(frame.area(), self.fullscreen);

        if self.fullscreen {
            self.list_area = Rect::default();
            self.daemons_area = Rect::default();
        } else {
            let rows = self.daemons.len().clamp(1, DAEMON_ROWS) as u16;
            let [daemons, sessions] =
                Layout::vertical([Constraint::Length(rows + 2), Constraint::Min(3)]).areas(left);
            self.draw_daemons(frame, daemons);
            self.draw_list(frame, sessions);
        }
        self.draw_output(frame, right);
        self.out_inner = Block::new().borders(Borders::ALL).inner(right);
        self.apply_selection(frame);
        self.draw_command(frame, command);

        let text = match (&self.input, &self.message) {
            (Some(input), _) => i18n::search_prompt(input),
            (None, Some(message)) => message.clone(),
            (None, None) => i18n::footer_hints().to_string(),
        };
        frame.render_widget(
            Paragraph::new(text).style(Style::new().fg(Color::DarkGray)),
            footer,
        );

        if self.show_help {
            self.draw_help(frame);
        }
    }

    /// Commande de la session sélectionnée, qu'elle tourne ou non.
    fn draw_command(&self, frame: &mut Frame, area: Rect) {
        // Une commande multiligne tient sur une seule ligne d'affichage.
        let command = self.current().command.trim().replace('\n', " ↵ ");
        let line = Line::from(vec![
            Span::styled("$ ", Style::new().fg(Color::Cyan)),
            Span::raw(command),
        ]);
        frame.render_widget(
            Paragraph::new(line).block(
                Block::new()
                    .borders(Borders::ALL)
                    .title(i18n::command_title()),
            ),
            area,
        );
    }

    fn draw_daemons(&mut self, frame: &mut Frame, area: Rect) {
        let items: Vec<ListItem> = self
            .daemons
            .iter()
            .map(|entry| {
                let current = entry.socket == self.socket;
                let (symbol, color) = if entry.running > 0 {
                    ("●", Color::Green)
                } else {
                    ("○", Color::DarkGray)
                };
                let name_style = if current {
                    Style::new().add_modifier(Modifier::BOLD)
                } else {
                    Style::new()
                };
                Line::from(vec![
                    Span::raw(if current { "▸ " } else { "  " }),
                    Span::styled(format!("{symbol} "), Style::new().fg(color)),
                    Span::styled(entry.label.clone(), name_style),
                    Span::styled(
                        format!("  {}", i18n::daemon_running(entry.running)),
                        Style::new().fg(Color::DarkGray),
                    ),
                ])
                .into()
            })
            .collect();
        let focused = self.focus == Focus::Daemons;
        let list = List::new(items)
            .block(
                Block::new()
                    .borders(Borders::ALL)
                    .border_style(focus_style(focused))
                    .title(i18n::daemons_title()),
            )
            .highlight_style(Style::new().add_modifier(Modifier::REVERSED));
        self.daemons_area = area;
        // Hors focus, seul le repère ▸ marque le daemon courant.
        self.daemons_state
            .select(focused.then_some(self.daemon_sel));
        frame.render_stateful_widget(list, area, &mut self.daemons_state);
    }

    fn draw_list(&mut self, frame: &mut Frame, area: Rect) {
        let mut items = Vec::new();
        self.list_rows.clear();
        let mut selected_row = 0;
        let mut last_group: Option<&str> = None;
        for (pos, session) in self.infos.iter().enumerate() {
            if last_group != Some(session.group.as_str()) {
                items.push(ListItem::new(Line::styled(
                    session.group.to_uppercase(),
                    Style::new().add_modifier(Modifier::BOLD).fg(Color::Cyan),
                )));
                self.list_rows.push(None);
                last_group = Some(session.group.as_str());
            }
            self.list_rows.push(Some(pos));
            if pos == self.selected {
                selected_row = items.len();
            }
            let (symbol, color, label) = status_display(session);
            items.push(ListItem::new(Line::from(vec![
                Span::styled(format!("{symbol} "), Style::new().fg(color)),
                Span::raw(session.name.clone()),
                Span::styled(format!("  {label}"), Style::new().fg(Color::DarkGray)),
            ])));
        }
        let list = List::new(items)
            .block(
                Block::new()
                    .borders(Borders::ALL)
                    .border_style(focus_style(self.focus == Focus::Sessions))
                    .title(i18n::sessions_title()),
            )
            .highlight_style(Style::new().add_modifier(Modifier::REVERSED));
        self.list_area = area;
        self.list_state.select(Some(selected_row));
        frame.render_stateful_widget(list, area, &mut self.list_state);
    }

    fn draw_output(&mut self, frame: &mut Frame, area: Rect) {
        let name = self.current().name.clone();
        let mut spans = vec![Span::raw(format!(" {name} "))];
        // Le titre démarre une colonne après le coin du cadre.
        let mut x = area.x + 1 + spans[0].width() as u16;
        self.tab_areas.clear();
        for tab in Tab::ALL {
            let label = format!(" {} ", tab.label());
            let width = label.chars().count() as u16;
            let style = if tab == self.tab {
                Style::new().add_modifier(Modifier::REVERSED | Modifier::BOLD)
            } else {
                Style::new().fg(Color::DarkGray)
            };
            self.tab_areas.push((tab, Rect::new(x, area.y, width, 1)));
            spans.push(Span::styled(label, style));
            x += width;
        }
        if self.tab == Tab::Output && !self.wrap {
            spans.push(Span::raw(format!(" · {} ", i18n::nowrap_tag())));
        }
        let block = Block::new().borders(Borders::ALL).title(Line::from(spans));
        match self.tab {
            Tab::Env => return self.draw_env(frame, area, block),
            Tab::Usage => return self.draw_usage(frame, area, block),
            Tab::Output => {}
        }
        let Some(view) = &mut self.view else {
            frame.render_widget(
                Paragraph::new(i18n::session_stopped_hint()).block(block),
                area,
            );
            return;
        };

        view.parser.screen_mut().set_scrollback(view.scroll);
        // set_scrollback borne la valeur : on la relit pour ne pas dépasser l'historique.
        view.scroll = view.parser.screen().scrollback();
        let inner = block.inner(area);
        let (_, cols) = view.parser.screen().size();
        let hscroll = if self.wrap {
            0
        } else {
            self.hscroll.min(cols.saturating_sub(inner.width) as usize)
        };
        self.hscroll = hscroll;
        if self.wrap {
            frame.render_widget(PseudoTerminal::new(view.parser.screen()).block(block), area);
        } else {
            frame.render_widget(block, area);
            draw_columns(frame, view.parser.screen(), inner, hscroll);
        }
        if let Some(query) = &view.query {
            let highlight = Style::new().bg(Color::Yellow).fg(Color::Black);
            for (row, text) in view.parser.screen().rows(0, cols).enumerate() {
                let y = inner.y + row as u16;
                if y >= inner.bottom() {
                    break;
                }
                for (start, len) in search::match_columns(&text, query) {
                    let Some(start) = start.checked_sub(hscroll) else {
                        continue;
                    };
                    let x = inner.x + start as u16;
                    if x < inner.right() {
                        let width = (len as u16).min(inner.right() - x);
                        frame
                            .buffer_mut()
                            .set_style(Rect::new(x, y, width, 1), highlight);
                    }
                }
            }
        }
        view.parser.screen_mut().set_scrollback(0);
    }

    fn draw_env(&mut self, frame: &mut Frame, area: Rect, block: Block) {
        let inner = block.inner(area);
        let max_scroll = self.env.len().saturating_sub(inner.height as usize);
        self.env_scroll = self.env_scroll.min(max_scroll);
        let lines: Vec<Line> = if self.env.is_empty() {
            vec![Line::styled(
                i18n::env_empty(),
                Style::new().fg(Color::DarkGray),
            )]
        } else {
            self.env
                .iter()
                .skip(self.env_scroll)
                .map(|(key, value)| {
                    Line::from(vec![
                        Span::styled(key.clone(), Style::new().fg(Color::Cyan)),
                        Span::styled("=", Style::new().fg(Color::DarkGray)),
                        // Une valeur multiligne reste sur une seule ligne d'affichage.
                        Span::raw(value.replace('\n', "\\n")),
                    ])
                })
                .collect()
        };
        frame.render_widget(Paragraph::new(lines).block(block), area);
    }

    /// Consommation actuelle de la session sélectionnée, relue dans `infos` (rafraîchie à chaque tour).
    fn draw_usage(&self, frame: &mut Frame, area: Rect, block: Block) {
        let info = self.current();
        let label =
            |text: &str| Span::styled(format!("{:<12}", text), Style::new().fg(Color::Cyan));
        let lines = match &info.usage {
            Some(usage) => vec![
                Line::from(vec![
                    label(i18n::usage_pid()),
                    Span::raw(info.pid.map_or("-".to_string(), |p| p.to_string())),
                ]),
                Line::from(vec![
                    label(i18n::usage_cpu()),
                    Span::raw(format!("{:.1} %", usage.cpu_percent)),
                ]),
                Line::from(vec![
                    label(i18n::usage_memory()),
                    Span::raw(crate::format_bytes(usage.memory_bytes)),
                ]),
                Line::from(vec![
                    label(i18n::usage_ports()),
                    Span::raw(crate::format_ports(&usage.ports)),
                ]),
                Line::from(vec![
                    label(i18n::usage_uptime()),
                    Span::raw(i18n::running(info.uptime.as_secs())),
                ]),
            ],
            None => vec![Line::styled(
                i18n::usage_empty(),
                Style::new().fg(Color::DarkGray),
            )],
        };
        frame.render_widget(Paragraph::new(lines).block(block), area);
    }

    fn draw_help(&self, frame: &mut Frame) {
        let text = i18n::help_text();
        let area = frame
            .area()
            .centered(Constraint::Length(74), Constraint::Length(25));
        frame.render_widget(ratatui::widgets::Clear, area);
        frame.render_widget(
            Paragraph::new(text)
                .block(Block::new().borders(Borders::ALL).title(i18n::help_title())),
            area,
        );
    }
}

/// Dessine la fenêtre de `area` sur l'écran émulé, à partir de la colonne `hscroll`.
fn draw_columns(frame: &mut Frame, screen: &vt100::Screen, area: Rect, hscroll: usize) {
    for y in 0..area.height {
        for x in 0..area.width {
            if let Some(cell) = screen.cell(y, hscroll as u16 + x) {
                cell.apply(&mut frame.buffer_mut()[(area.x + x, area.y + y)]);
            }
        }
    }
}

/// Cadre du panneau qui a le focus : cyan, les autres gardent le style par défaut.
fn focus_style(focused: bool) -> Style {
    if focused {
        Style::new().fg(Color::Cyan)
    } else {
        Style::new()
    }
}

/// Libellé d'un daemon : les `depth` derniers dossiers du chemin de sa config.
fn daemon_label(config: &Path, depth: usize) -> String {
    let parts: Vec<&str> = config
        .parent()
        .map(|dir| {
            dir.components()
                .filter_map(|c| c.as_os_str().to_str())
                .collect()
        })
        .unwrap_or_default();
    let start = parts.len().saturating_sub(depth);
    let label = parts[start..].join("/");
    if label.is_empty() || label == "/" {
        config.display().to_string()
    } else {
        label
    }
}

fn status_display(session: &SessionInfo) -> (&'static str, Color, String) {
    match session.state {
        State::NotStarted => ("○", Color::DarkGray, i18n::stopped().into()),
        State::Running if session.outdated => (
            "●",
            Color::Green,
            format!("{} ~", format_duration(session.uptime.as_secs())),
        ),
        State::Running => ("●", Color::Green, format_duration(session.uptime.as_secs())),
        State::Stopping => ("◌", Color::Yellow, i18n::stopping().into()),
        State::Exited(0) => ("○", Color::DarkGray, i18n::finished().into()),
        State::Exited(code) => ("●", Color::Red, i18n::failed(code)),
    }
}

fn format_duration(secs: u64) -> String {
    match secs {
        0..60 => format!("{secs}s"),
        60..3600 => format!("{}m{:02}s", secs / 60, secs % 60),
        _ => format!("{}h{:02}m", secs / 3600, secs % 3600 / 60),
    }
}

/// Copie `text` dans le presse-papiers : séquence OSC 52 (comprise par la plupart des
/// terminaux, dont Windows Terminal sous WSL) et, en renfort, un utilitaire système s'il existe.
fn copy_to_clipboard(text: &str) {
    use std::io::Write;
    let mut out = std::io::stdout();
    let _ = write!(out, "\x1b]52;c;{}\x07", base64(text.as_bytes()));
    let _ = out.flush();
    // Sous WSL, le presse-papiers Windows : le texte passe en base64 pour garder UTF-8 et les
    // retours à la ligne. La ligne de commande Windows est limitée à ~32 000 caractères.
    let encoded = base64(text.as_bytes());
    if encoded.len() < 24_000 {
        let script = format!(
            "Set-Clipboard -Value ([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('{encoded}')))"
        );
        let child = std::process::Command::new("powershell.exe")
            .args(["-NoProfile", "-NonInteractive", "-Command", &script])
            .stdin(std::process::Stdio::null())
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn();
        if let Ok(mut child) = child {
            // Attendu dans un thread pour ne pas bloquer la TUI ni laisser de processus zombie.
            std::thread::spawn(move || {
                let _ = child.wait();
            });
            return;
        }
    }
    for (program, args) in [
        ("wl-copy", &[][..]),
        ("xclip", &["-selection", "clipboard"][..]),
        ("xsel", &["-ib"][..]),
    ] {
        let child = std::process::Command::new(program)
            .args(args)
            .stdin(std::process::Stdio::piped())
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .spawn();
        if let Ok(mut child) = child {
            if let Some(mut stdin) = child.stdin.take() {
                let _ = stdin.write_all(text.as_bytes());
            }
            // Ces outils restent parfois en arrière-plan pour servir la sélection : on ne les attend pas.
            return;
        }
    }
}

fn base64(data: &[u8]) -> String {
    const TABLE: &[u8; 64] = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
    let mut out = String::with_capacity(data.len().div_ceil(3) * 4);
    for chunk in data.chunks(3) {
        let n = chunk
            .iter()
            .enumerate()
            .fold(0u32, |acc, (i, &b)| acc | (b as u32) << (16 - 8 * i));
        for i in 0..4 {
            if i <= chunk.len() {
                out.push(TABLE[(n >> (18 - 6 * i) & 63) as usize] as char);
            } else {
                out.push('=');
            }
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::base64;

    #[test]
    fn base64_matches_rfc4648() {
        assert_eq!(base64(b""), "");
        assert_eq!(base64(b"f"), "Zg==");
        assert_eq!(base64(b"fo"), "Zm8=");
        assert_eq!(base64(b"foo"), "Zm9v");
        assert_eq!(base64(b"foobar"), "Zm9vYmFy");
    }
}
