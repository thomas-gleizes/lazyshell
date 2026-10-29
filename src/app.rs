use std::path::{Path, PathBuf};
use std::time::Duration;

use anyhow::{Context, Result};
use crossterm::event::{
    self, DisableMouseCapture, EnableMouseCapture, Event, KeyCode, KeyEvent, KeyEventKind, KeyModifiers,
    MouseEventKind,
};
use crossterm::execute;
use ratatui::layout::{Constraint, Layout, Rect};
use ratatui::style::{Color, Modifier, Style};
use ratatui::text::{Line, Span};
use ratatui::widgets::{Block, Borders, List, ListItem, ListState, Paragraph};
use ratatui::{DefaultTerminal, Frame};
use tui_term::vt100;
use tui_term::widget::PseudoTerminal;

use crate::client::{Client, Stream, StreamEvent};
use crate::daemon::{Command, SessionInfo, State, Target};
use crate::{i18n, search};

const PAGE_SCROLL: usize = 10;
const WHEEL_SCROLL: usize = 3;
const SCROLLBACK_LINES: usize = 5000;
/// Évents de flux traités par tour de boucle, pour que l'interface reste réactive.
const EVENTS_PER_TICK: usize = 256;

/// Sortie de la session affichée : flux du daemon, écran émulé et défilement local.
struct View {
    session: String,
    run_id: u64,
    stream: Stream,
    parser: vt100::Parser,
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

struct App {
    socket: PathBuf,
    control: Client,
    /// Copie de l'état des sessions, rafraîchie à chaque tour de boucle.
    infos: Vec<SessionInfo>,
    selected: usize,
    view: Option<View>,
    /// Saisie de recherche en cours (`/`).
    input: Option<String>,
    show_help: bool,
    message: Option<String>,
    quit: bool,
    shutdown_requested: bool,
}

pub fn run(socket: &Path) -> Result<()> {
    let mut control = Client::connect(socket)?;
    let mut app = App {
        infos: control.list()?,
        control,
        socket: socket.to_path_buf(),
        selected: 0,
        view: None,
        input: None,
        show_help: false,
        message: None,
        quit: false,
        shutdown_requested: false,
    };
    let mut terminal = ratatui::init();
    execute!(std::io::stdout(), EnableMouseCapture)?;
    let result = app.event_loop(&mut terminal);
    let _ = execute!(std::io::stdout(), DisableMouseCapture);
    ratatui::restore();
    result
}

fn layout(area: Rect) -> (Rect, Rect, Rect) {
    let [main, footer] =
        Layout::vertical([Constraint::Min(1), Constraint::Length(1)]).areas(area);
    let [left, right] =
        Layout::horizontal([Constraint::Length(32), Constraint::Min(20)]).areas(main);
    (left, right, footer)
}

/// Taille (lignes, colonnes) utile du panneau de sortie pour un terminal donné.
fn output_size(area: Rect) -> (u16, u16) {
    let (_, right, _) = layout(area);
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
            let size = terminal.size()?;
            self.sync_view(output_size(Rect::new(0, 0, size.width, size.height)));
            terminal.draw(|frame| self.draw(frame))?;

            if self.shutdown_requested {
                self.control.shutdown()?;
                break;
            }
            if event::poll(Duration::from_millis(50))? {
                match event::read()? {
                    Event::Key(key) if key.kind == KeyEventKind::Press => self.on_key(key),
                    Event::Mouse(mouse) => match mouse.kind {
                        MouseEventKind::ScrollUp => self.scroll(|offset| offset + WHEEL_SCROLL),
                        MouseEventKind::ScrollDown => {
                            self.scroll(|offset| offset.saturating_sub(WHEEL_SCROLL))
                        }
                        _ => {}
                    },
                    _ => {}
                }
            }
        }
        Ok(())
    }

    /// Aligne la vue sur la session sélectionnée et son exécution courante, applique
    /// la taille du panneau et absorbe le flux reçu.
    fn sync_view(&mut self, size: (u16, u16)) {
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
            KeyCode::Char('/') => self.input = Some(String::new()),
            KeyCode::Char('n') => self.jump(Jump::Older),
            KeyCode::Char('N') => self.jump(Jump::Newer),
            KeyCode::Esc => {
                if let Some(view) = &mut self.view {
                    view.query = None;
                    view.found_at = None;
                }
            }
            KeyCode::Char('?') => self.show_help = true,
            KeyCode::Char('j') | KeyCode::Down => self.selected = (self.selected + 1).min(last),
            KeyCode::Char('k') | KeyCode::Up => self.selected = self.selected.saturating_sub(1),
            KeyCode::Char('s') => self.send(Command::Start(session())),
            KeyCode::Char('x') => self.send(Command::Stop(session())),
            KeyCode::Char('r') => self.send(Command::Restart(session())),
            KeyCode::Char('S') => self.send(Command::Start(group())),
            KeyCode::Char('X') => self.send(Command::Stop(group())),
            KeyCode::Char('R') => self.send(Command::Restart(group())),
            KeyCode::PageUp => self.scroll(|offset| offset + PAGE_SCROLL),
            KeyCode::PageDown => self.scroll(|offset| offset.saturating_sub(PAGE_SCROLL)),
            KeyCode::Char('g') => self.scroll(|_| usize::MAX / 2),
            KeyCode::Char('G') => self.scroll(|_| 0),
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

    fn scroll(&mut self, update: impl FnOnce(usize) -> usize) {
        if let Some(view) = &mut self.view {
            view.scroll = update(view.scroll);
        }
    }

    fn send(&mut self, command: Command) {
        if let Err(err) = self.control.command(command) {
            self.message = Some(err.to_string());
        }
    }

    fn draw(&mut self, frame: &mut Frame) {
        let (left, right, footer) = layout(frame.area());

        self.draw_list(frame, left);
        self.draw_output(frame, right);

        let text = match (&self.input, &self.message) {
            (Some(input), _) => i18n::search_prompt(input),
            (None, Some(message)) => message.clone(),
            (None, None) => {
                i18n::footer_hints().to_string()
            }
        };
        frame.render_widget(Paragraph::new(text).style(Style::new().fg(Color::DarkGray)), footer);

        if self.show_help {
            self.draw_help(frame);
        }
    }

    fn draw_list(&self, frame: &mut Frame, area: Rect) {
        let mut items = Vec::new();
        let mut selected_row = 0;
        let mut last_group: Option<&str> = None;
        for (pos, session) in self.infos.iter().enumerate() {
            if last_group != Some(session.group.as_str()) {
                items.push(ListItem::new(Line::styled(
                    session.group.to_uppercase(),
                    Style::new().add_modifier(Modifier::BOLD).fg(Color::Cyan),
                )));
                last_group = Some(session.group.as_str());
            }
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
            .block(Block::new().borders(Borders::ALL).title(i18n::sessions_title()))
            .highlight_style(Style::new().add_modifier(Modifier::REVERSED));
        let mut state = ListState::default().with_selected(Some(selected_row));
        frame.render_stateful_widget(list, area, &mut state);
    }

    fn draw_output(&mut self, frame: &mut Frame, area: Rect) {
        let block = Block::new().borders(Borders::ALL).title(format!(" {} ", self.current().name));
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
        frame.render_widget(PseudoTerminal::new(view.parser.screen()).block(block), area);
        if let Some(query) = &view.query {
            let highlight = Style::new().bg(Color::Yellow).fg(Color::Black);
            let (_, cols) = view.parser.screen().size();
            for (row, text) in view.parser.screen().rows(0, cols).enumerate() {
                let y = inner.y + row as u16;
                if y >= inner.bottom() {
                    break;
                }
                for (start, len) in search::match_columns(&text, query) {
                    let x = inner.x + start as u16;
                    if x < inner.right() {
                        let width = (len as u16).min(inner.right() - x);
                        frame.buffer_mut().set_style(Rect::new(x, y, width, 1), highlight);
                    }
                }
            }
        }
        view.parser.screen_mut().set_scrollback(0);
    }

    fn draw_help(&self, frame: &mut Frame) {
        let text = i18n::help_text();
        let area = frame.area().centered(Constraint::Length(74), Constraint::Length(19));
        frame.render_widget(ratatui::widgets::Clear, area);
        frame.render_widget(
            Paragraph::new(text).block(Block::new().borders(Borders::ALL).title(i18n::help_title())),
            area,
        );
    }
}

fn status_display(session: &SessionInfo) -> (&'static str, Color, String) {
    match session.state {
        State::NotStarted => ("○", Color::DarkGray, i18n::stopped().into()),
        State::Running if session.outdated => {
            ("●", Color::Green, format!("{} ~", format_duration(session.uptime.as_secs())))
        }
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
