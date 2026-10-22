package main

import (
	"fmt"
	"strconv"
	"strings"
)

type Token struct {
	Type  string
	Value interface{} // string for most, int64/float64 for NUMBER, bool for TRUE/FALSE
	Line  int
	Col   int
}

var keywords = map[string]bool{
	"if": true, "elif": true, "else": true, "while": true, "for": true,
	"def": true, "class": true, "return": true, "print": true, "say": true,
	"true": true, "false": true, "null": true, "None": true, "in": true,
	"to": true, "and": true, "or": true, "not": true, "is": true,
	"import": true, "from": true, "as": true, "try": true, "catch": true,
	"except": true, "finally": true, "raise": true, "break": true,
	"continue": true, "pass": true, "lambda": true, "fn": true,
	"forta": true, "nesejo": true, "shfaq": true, "retroc": true,
	"remember": true,
}

func isDigit(c rune) bool { return c >= '0' && c <= '9' }
func isAlpha(c rune) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isAlnum(c rune) bool { return isAlpha(c) || isDigit(c) }

func syntaxErr(format string, args ...interface{}) *AlbError {
	return &AlbError{Kind: "SyntaxError", Msg: fmt.Sprintf(format, args...)}
}

// unescapeString mirrors Python's encode('utf-8').decode('unicode_escape'):
// bytes are processed one at a time; non-escape bytes become code points of
// the same value (which intentionally reproduces the original's behavior
// for non-ASCII text, exactly like the Python interpreter).
func unescapeString(raw string) string {
	bytes := []byte(raw)
	var out []rune
	i := 0
	for i < len(bytes) {
		b := bytes[i]
		if b != '\\' || i+1 >= len(bytes) {
			out = append(out, rune(b))
			i++
			continue
		}
		i++
		c := bytes[i]
		i++
		switch c {
		case 'n':
			out = append(out, '\n')
		case 't':
			out = append(out, '\t')
		case 'r':
			out = append(out, '\r')
		case 'a':
			out = append(out, 7)
		case 'b':
			out = append(out, 8)
		case 'f':
			out = append(out, 12)
		case 'v':
			out = append(out, 11)
		case '0', '1', '2', '3', '4', '5', '6', '7':
			val := int(c - '0')
			for n := 0; n < 2 && i < len(bytes) && bytes[i] >= '0' && bytes[i] <= '7'; n++ {
				val = val*8 + int(bytes[i]-'0')
				i++
			}
			out = append(out, rune(val))
		case 'x':
			if i+1 < len(bytes) && isHex(bytes[i]) && isHex(bytes[i+1]) {
				out = append(out, rune(hexVal(bytes[i])*16+hexVal(bytes[i+1])))
				i += 2
			} else {
				out = append(out, '\\', 'x')
			}
		case 'u':
			if i+3 < len(bytes) && isHex(bytes[i]) && isHex(bytes[i+1]) && isHex(bytes[i+2]) && isHex(bytes[i+3]) {
				v := 0
				for k := 0; k < 4; k++ {
					v = v*16 + hexVal(bytes[i+k])
				}
				out = append(out, rune(v))
				i += 4
			} else {
				out = append(out, '\\', 'u')
			}
		case 'U':
			if i+7 < len(bytes) {
				ok := true
				v := 0
				for k := 0; k < 8; k++ {
					if !isHex(bytes[i+k]) {
						ok = false
						break
					}
					v = v*16 + hexVal(bytes[i+k])
				}
				if ok && v <= 0x10FFFF {
					out = append(out, rune(v))
					i += 8
				} else {
					out = append(out, '\\', 'U')
				}
			} else {
				out = append(out, '\\', 'U')
			}
		case '\n':
			// line continuation: emits nothing
		case '\\', '\'', '"':
			out = append(out, rune(c))
		default:
			out = append(out, '\\', rune(c))
		}
	}
	return string(out)
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}
func hexVal(b byte) int {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0')
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10
	}
	return int(b-'A') + 10
}

var twoCharOps = []string{
	"==", "!=", "<=", ">=", "**", "//", "+=", "-=", "*=", "/=", "%=",
	"&=", "|=", "^=", "<<", ">>",
}
var threeCharOps = []string{"<<=", ">>="}

func tokenize(code string) []Token {
	src := []rune(code)
	var tokens []Token
	line, col, pos := 1, 1, 0
	n := len(src)
	advance := func(count int) {
		for k := 0; k < count && pos < n; k++ {
			if src[pos] == '\n' {
				line++
				col = 1
			} else {
				col++
			}
			pos++
		}
	}
	for pos < n {
		// block comments
		if src[pos] == '/' && pos+1 < n && src[pos+1] == '*' {
			startLine := line
			end := -1
			for j := pos + 2; j+1 < n; j++ {
				if src[j] == '*' && src[j+1] == '/' {
					end = j
					break
				}
			}
			if end == -1 {
				panic(syntaxErr("Unterminated block comment at line %d", startLine))
			}
			advance(end + 2 - pos)
			continue
		}
		c := src[pos]
		startCol := col
		switch {
		case c == '\n':
			tokens = append(tokens, Token{"NEWLINE", "\n", line, startCol})
			advance(1)
		case c == ' ' || c == '\t':
			advance(1)
		case c == '#':
			for pos < n && src[pos] != '\n' {
				advance(1)
			}
		case isDigit(c):
			start := pos
			for pos < n && isDigit(src[pos]) {
				advance(1)
			}
			isFloat := false
			if pos < n && src[pos] == '.' && pos+1 < n && isDigit(src[pos+1]) {
				isFloat = true
				advance(1)
				for pos < n && isDigit(src[pos]) {
					advance(1)
				}
			}
			if pos < n && (src[pos] == 'e' || src[pos] == 'E') {
				j := pos + 1
				if j < n && (src[j] == '+' || src[j] == '-') {
					j++
				}
				if j < n && isDigit(src[j]) {
					isFloat = true
					advance(j - pos)
					for pos < n && isDigit(src[pos]) {
						advance(1)
					}
				}
			}
			text := string(src[start:pos])
			if isFloat {
				f, _ := strconv.ParseFloat(text, 64)
				tokens = append(tokens, Token{"NUMBER", f, line, startCol})
			} else {
				if iv, err := strconv.ParseInt(text, 10, 64); err == nil {
					tokens = append(tokens, Token{"NUMBER", iv, line, startCol})
				} else {
					f, _ := strconv.ParseFloat(text, 64)
					tokens = append(tokens, Token{"NUMBER", f, line, startCol})
				}
			}
		case c == '"' || c == '\'':
			quote := c
			triple := pos+2 < n && src[pos+1] == quote && src[pos+2] == quote
			var raw string
			if triple {
				j := pos + 3
				endIdx := -1
				for j+2 < n {
					if src[j] == quote && src[j+1] == quote && src[j+2] == quote {
						endIdx = j
						break
					}
					j++
				}
				if endIdx == -1 {
					// regex would fail to match -> STRING wouldn't match; the
					// quote char itself becomes MISMATCH in the original.
					panic(syntaxErr("Unexpected character %q at %d:%d", string(quote), line, startCol))
				}
				raw = string(src[pos+3 : endIdx])
				advance(endIdx + 3 - pos)
			} else {
				j := pos + 1
				var sb strings.Builder
				closed := false
				for j < n {
					if src[j] == '\\' && j+1 < n {
						sb.WriteRune(src[j])
						sb.WriteRune(src[j+1])
						j += 2
						continue
					}
					if src[j] == quote {
						closed = true
						break
					}
					sb.WriteRune(src[j])
					j++
				}
				if !closed {
					panic(syntaxErr("Unexpected character %q at %d:%d", string(quote), line, startCol))
				}
				raw = sb.String()
				advance(j + 1 - pos)
			}
			tokens = append(tokens, Token{"STRING", unescapeString(raw), line, startCol})
		case isAlpha(c):
			start := pos
			for pos < n && isAlnum(src[pos]) {
				advance(1)
			}
			word := string(src[start:pos])
			if keywords[word] {
				switch word {
				case "true":
					tokens = append(tokens, Token{"TRUE", true, line, startCol})
				case "false":
					tokens = append(tokens, Token{"FALSE", false, line, startCol})
				case "null", "None":
					tokens = append(tokens, Token{"NULL", nil, line, startCol})
				default:
					tokens = append(tokens, Token{strings.ToUpper(word), word, line, startCol})
				}
			} else {
				tokens = append(tokens, Token{"ID", word, line, startCol})
			}
		default:
			rest := string(src[pos:])
			matched := ""
			for _, op := range threeCharOps {
				if strings.HasPrefix(rest, op) {
					matched = op
					break
				}
			}
			if matched == "" {
				for _, op := range twoCharOps {
					if strings.HasPrefix(rest, op) {
						matched = op
						break
					}
				}
			}
			if matched != "" {
				tokens = append(tokens, Token{"OP", matched, line, startCol})
				advance(len([]rune(matched)))
				continue
			}
			switch c {
			case '{':
				tokens = append(tokens, Token{"LBRACE", "{", line, startCol})
			case '}':
				tokens = append(tokens, Token{"RBRACE", "}", line, startCol})
			case '(':
				tokens = append(tokens, Token{"LPAREN", "(", line, startCol})
			case ')':
				tokens = append(tokens, Token{"RPAREN", ")", line, startCol})
			case '[':
				tokens = append(tokens, Token{"LBRACK", "[", line, startCol})
			case ']':
				tokens = append(tokens, Token{"RBRACK", "]", line, startCol})
			case ',':
				tokens = append(tokens, Token{"COMMA", ",", line, startCol})
			case ':':
				tokens = append(tokens, Token{"COLON", ":", line, startCol})
			case '.':
				tokens = append(tokens, Token{"DOT", ".", line, startCol})
			case ';':
				tokens = append(tokens, Token{"SEMI", ";", line, startCol})
			case '+', '-', '*', '/', '%', '<', '>', '=', '!', '&', '|', '^', '~':
				tokens = append(tokens, Token{"OP", string(c), line, startCol})
			default:
				panic(syntaxErr("Unexpected character %q at %d:%d", string(c), line, startCol))
			}
			advance(1)
		}
	}
	tokens = append(tokens, Token{"EOF", nil, line, col})
	return tokens
}
