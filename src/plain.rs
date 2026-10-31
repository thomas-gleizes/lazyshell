//! Mise en texte brut de la sortie d'un PTY pour la CLI (agents, scripts) : retire les
//! séquences d'échappement ANSI et normalise les fins de ligne.

#[derive(Clone, Copy, PartialEq, Eq)]
enum State {
    Text,
    /// Après `ESC`.
    Escape,
    /// Dans une séquence `ESC [ … <octet final>`.
    Csi,
    /// Dans une chaîne `ESC ] … (BEL | ESC \)` (titre de fenêtre, lien…).
    Osc,
    /// `ESC` rencontré dans une chaîne OSC : attend `\`.
    OscEscape,
    /// Séquence `ESC <intermédiaire> <final>` (ex. `ESC ( B`) : un octet de plus à ignorer.
    Skip,
}

/// Filtre à état : la sortie arrive par morceaux, une séquence peut être coupée entre deux.
pub struct Stripper {
    state: State,
    /// Le dernier octet émis était un `\r` en attente (devenu `\n` s'il n'est pas suivi de `\n`).
    pending_cr: bool,
}

impl Stripper {
    pub fn new() -> Self {
        Stripper { state: State::Text, pending_cr: false }
    }

    pub fn feed(&mut self, input: &[u8]) -> Vec<u8> {
        let mut out = Vec::with_capacity(input.len());
        for &byte in input {
            match self.state {
                State::Text => self.text(byte, &mut out),
                State::Escape => {
                    self.state = match byte {
                        b'[' => State::Csi,
                        b']' => State::Osc,
                        0x20..=0x2f => State::Skip,
                        _ => State::Text,
                    }
                }
                State::Csi => {
                    if (0x40..=0x7e).contains(&byte) {
                        self.state = State::Text;
                    }
                }
                State::Osc => match byte {
                    0x07 => self.state = State::Text,
                    0x1b => self.state = State::OscEscape,
                    _ => {}
                },
                State::OscEscape => self.state = if byte == b'\\' { State::Text } else { State::Osc },
                State::Skip => self.state = State::Text,
            }
        }
        out
    }

    fn text(&mut self, byte: u8, out: &mut Vec<u8>) {
        // Un `\r` seul (barre de progression qui se réécrit) devient un saut de ligne ;
        // `\r\n` n'en donne qu'un.
        if self.pending_cr {
            self.pending_cr = false;
            out.push(b'\n');
            if byte == b'\n' {
                return;
            }
        }
        match byte {
            0x1b => self.state = State::Escape,
            b'\r' => self.pending_cr = true,
            b'\n' | b'\t' => out.push(byte),
            // Autres caractères de contrôle (BEL, backspace…) : ignorés. Les octets UTF-8 passent.
            0x00..=0x1f | 0x7f => {}
            _ => out.push(byte),
        }
    }
}

/// Les `n` dernières lignes de `text` (la fin de ligne finale ne compte pas comme une ligne).
pub fn last_lines(text: &[u8], n: usize) -> &[u8] {
    if n == 0 {
        return &[];
    }
    let body = text.strip_suffix(b"\n").unwrap_or(text);
    let mut seen = 0;
    for (index, &byte) in body.iter().enumerate().rev() {
        if byte == b'\n' {
            seen += 1;
            if seen == n {
                return &text[index + 1..];
            }
        }
    }
    text
}

#[cfg(test)]
mod tests {
    use super::*;

    fn strip(chunks: &[&[u8]]) -> String {
        let mut stripper = Stripper::new();
        let out: Vec<u8> = chunks.iter().flat_map(|c| stripper.feed(c)).collect();
        String::from_utf8(out).unwrap()
    }

    #[test]
    fn removes_colors_and_osc() {
        assert_eq!(strip(&[b"\x1b[31mrouge\x1b[0m ok\r\n"]), "rouge ok\n");
        assert_eq!(strip(&[b"\x1b]0;titre\x07texte\x1b]8;;u\x1b\\!"]), "texte!");
        assert_eq!(strip(&["é\x1b(B".as_bytes()]), "é");
    }

    #[test]
    fn sequences_may_span_chunks() {
        assert_eq!(strip(&[b"a\x1b[3", b"1mb\r", b"\nc"]), "ab\nc");
    }

    #[test]
    fn lone_cr_becomes_newline() {
        assert_eq!(strip(&[b"10%\r50%\r100%\n"]), "10%\n50%\n100%\n");
    }

    #[test]
    fn tail_lines() {
        assert_eq!(last_lines(b"a\nb\nc\n", 2), b"b\nc\n");
        assert_eq!(last_lines(b"a\nb\nc", 2), b"b\nc");
        assert_eq!(last_lines(b"a\nb\n", 10), b"a\nb\n");
        assert_eq!(last_lines(b"a\nb\n", 0), b"");
    }
}
