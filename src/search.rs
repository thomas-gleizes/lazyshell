//! Recherche dans la sortie d'une session, à partir de l'écran émulé et de son historique.

use tui_term::vt100;

fn fold(c: char) -> char {
    c.to_ascii_lowercase()
}

/// Toutes les lignes de l'historique puis de l'écran, de la plus ancienne à la plus récente.
/// Laisse l'affichage sur le direct (défilement 0).
pub fn history_lines(parser: &mut vt100::Parser) -> Vec<String> {
    let (rows, cols) = parser.screen().size();
    let (rows, total) = {
        parser.screen_mut().set_scrollback(usize::MAX);
        (rows as usize, parser.screen().scrollback())
    };

    let mut lines = Vec::with_capacity(total + rows);
    let mut start = 0;
    while start < total + rows {
        // Au défilement `s`, l'écran montre les lignes [total - s, total - s + rows).
        let scrollback = total.saturating_sub(start);
        parser.screen_mut().set_scrollback(scrollback);
        let skip = start.saturating_sub(total);
        lines.extend(parser.screen().rows(0, cols).skip(skip));
        start += rows;
    }
    parser.screen_mut().set_scrollback(0);
    lines
}

/// Indices des lignes contenant `query` (sans tenir compte de la casse ASCII), croissants.
pub fn matching_lines(lines: &[String], query: &str) -> Vec<usize> {
    lines.iter().enumerate().filter(|(_, line)| !match_columns(line, query).is_empty()).map(|(i, _)| i).collect()
}

/// Positions (colonne de départ, longueur) des occurrences de `query` dans une ligne.
pub fn match_columns(row: &str, query: &str) -> Vec<(usize, usize)> {
    let hay: Vec<char> = row.chars().map(fold).collect();
    let needle: Vec<char> = query.chars().map(fold).collect();
    let mut found = Vec::new();
    if needle.is_empty() || needle.len() > hay.len() {
        return found;
    }
    let mut i = 0;
    while i + needle.len() <= hay.len() {
        if hay[i..i + needle.len()] == needle[..] {
            found.push((i, needle.len()));
            i += needle.len();
        } else {
            i += 1;
        }
    }
    found
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn finds_ignoring_case() {
        assert_eq!(match_columns("Hello hello", "HELLO"), vec![(0, 5), (6, 5)]);
        assert!(match_columns("abc", "").is_empty());
    }

    #[test]
    fn history_covers_scrollback_and_screen() {
        let mut parser = vt100::Parser::new(5, 20, 100);
        for i in 0..23 {
            parser.process(format!("ligne {i}\r\n").as_bytes());
        }
        let lines = history_lines(&mut parser);
        for i in 0..23 {
            assert_eq!(lines.iter().filter(|l| l.trim_end() == format!("ligne {i}")).count(), 1, "ligne {i}");
        }
        assert_eq!(matching_lines(&lines, "ligne 22").len(), 1);
    }
}
