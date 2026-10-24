// Package oracle is a client of the reference's oracle protocol (its
// docs/oracle-protocol.md, protocol 1.0): the extracted proof models run as a
// process, one JSON request per line on its standard input, one JSON response per
// line on its standard output, in order. A host's fuzzer asks the models about
// inputs nobody chose and compares each answer with its own port, which is the
// differential the host-compliance class oracle-differential records.
//
// The client belongs in test and fuzz tooling only, never on a host's runtime
// surface. It is a package rather than a test helper so a Go host built on this
// spine can drive the same oracle against its own code.
package oracle

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/Fuaran-Core/fuaran-core-go/json"
)

// Client is one oracle process, serving requests in order until Close.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	closed bool
}

// Start launches the oracle as the command given (the self-contained binary, or
// `dotnet <Fuaran.Core.Oracle.Cli.dll>` when running from a Core checkout).
func Start(name string, args ...string) (*Client, error) {
	cmd := exec.Command(name, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Client{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 1<<20)}, nil
}

// StartCommandLine splits a command line on spaces (the shape an environment
// variable carries, such as `dotnet C:/core/tools/.../Fuaran.Core.Oracle.Cli.dll`)
// and starts it.
func StartCommandLine(line string) (*Client, error) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil, errors.New("oracle: an empty command line")
	}
	return Start(fields[0], fields[1:]...)
}

// Close ends the oracle's input and waits for it to exit.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	_ = c.stdin.Close()
	return c.cmd.Wait()
}

// ErrorResponse is the oracle's error shape: no model answered. Its Code is one of
// malformed-request, unknown-function, bad-input or internal, and a harness counts
// each as "no verdict", never as a disagreement.
type ErrorResponse struct {
	Code    string
	Message string
}

func (e *ErrorResponse) Error() string { return "oracle " + e.Code + ": " + e.Message }

// Call asks fn of the oracle with input, given as the request's `input` member in
// canonical JSON text, and answers the `answer` member as a parsed value. A
// protocol error is an *ErrorResponse; a dead process or an unreadable line is any
// other error, and never a verdict.
func (c *Client) Call(fn string, inputJSON string) (json.Value, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("oracle: closed")
	}
	request := `{"fn":` + json.EscapeString(fn) + `,"input":` + inputJSON + "}\n"
	if _, err := io.WriteString(c.stdin, request); err != nil {
		return nil, fmt.Errorf("oracle: writing a request: %w", err)
	}
	line, err := c.stdout.ReadString('\n')
	if err != nil && line == "" {
		return nil, fmt.Errorf("oracle: no response (%w)", err)
	}
	line = strings.TrimRight(line, "\r\n")
	v, r := json.Parse(tolerateLoneSurrogates(line))
	if r != nil {
		return nil, fmt.Errorf("oracle: a response that is not JSON: %v", r)
	}
	if e := json.Field(v, "error"); e != nil {
		code, _ := json.Field(e, "code").(json.String)
		message, _ := json.Field(e, "message").(json.String)
		return nil, &ErrorResponse{Code: string(code), Message: string(message)}
	}
	answer := json.Field(v, "answer")
	if answer == nil {
		return nil, errors.New("oracle: a response with neither answer nor error")
	}
	return answer, nil
}

// tolerateLoneSurrogates rewrites each UNPAIRED surrogate escape in a response line
// to U+FFFD. The protocol lets a response carry one (the oracle's string model is
// UTF-16 units, so a refusal's message can quote half of a character), and the
// spine's reader refuses it as every conformant reader must; a client has no use for
// the unit itself, only for the members around it.
func tolerateLoneSurrogates(line string) string {
	isEscape := func(i int) (uint16, bool) {
		if i+6 > len(line) || line[i] != '\\' || line[i+1] != 'u' {
			return 0, false
		}
		u, err := strconv.ParseUint(line[i+2:i+6], 16, 16)
		return uint16(u), err == nil
	}
	var out strings.Builder
	for i := 0; i < len(line); {
		if line[i] == '\\' && i+1 < len(line) && line[i+1] != 'u' {
			// A short escape (`\\`, `\"`, `\n`, ...): copied whole, so the character after
			// an escaped backslash is never read as the start of an escape.
			out.WriteString(line[i : i+2])
			i += 2
			continue
		}
		u, ok := isEscape(i)
		if !ok {
			out.WriteByte(line[i])
			i++
			continue
		}
		switch {
		case u >= 0xD800 && u <= 0xDBFF:
			if low, ok := isEscape(i + 6); ok && low >= 0xDC00 && low <= 0xDFFF {
				out.WriteString(line[i : i+12])
				i += 12
				continue
			}
			out.WriteString(`�`)
		case u >= 0xDC00 && u <= 0xDFFF:
			out.WriteString(`�`)
		default:
			out.WriteString(line[i : i+6])
		}
		i += 6
	}
	return out.String()
}

// Describe asks oracle.describe and answers the protocol version it serves.
func (c *Client) Describe() (protocol string, err error) {
	answer, err := c.Call("oracle.describe", "{}")
	if err != nil {
		return "", err
	}
	p, ok := json.Field(json.Field(answer, "description"), "protocol").(json.String)
	if !ok {
		return "", errors.New("oracle: describe answered no protocol")
	}
	return string(p), nil
}
