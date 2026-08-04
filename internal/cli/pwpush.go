package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Rishang/cloudutil/internal/ui"
)

// pwpushConfig is persisted at ~/.config/cu/psst.json.
type pwpushConfig struct {
	Token  string `json:"token"`
	Source string `json:"source"`
	Email  string `json:"email,omitempty"`
}

func pwpushConfigPath() string { return filepath.Join(configHome(), "psst.json") }

func loadPwpushConfig() (*pwpushConfig, error) {
	data, err := os.ReadFile(pwpushConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no configuration found — run 'cu pwpush config' first")
		}
		return nil, err
	}
	cfg := &pwpushConfig{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("could not parse %s: %w", pwpushConfigPath(), err)
	}
	return cfg, nil
}

// headers picks Bearer auth for pwpush.com and legacy header auth for
// self-hosted instances configured with an email.
func (c *pwpushConfig) headers() map[string]string {
	if c.Email != "" {
		return map[string]string{
			"X-User-Email": c.Email,
			"X-User-Token": c.Token,
			"Accept":       "application/json",
		}
	}
	return map[string]string{
		"Authorization": "Bearer " + c.Token,
		"Accept":        "application/json",
	}
}

func newPwpushCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pwpush",
		Short: "Password Pusher commands — https://docs.pwpush.com/",
	}
	cmd.AddCommand(
		newPwpushConfigCommand(),
		newPwpushListActiveCommand(),
		newPwpushSendCommand(),
		newPwgenCommand(),
	)
	return cmd
}

func newPwpushConfigCommand() *cobra.Command {
	cfg := &pwpushConfig{}

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Save your Password Pusher token and instance URL",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg.Source = strings.TrimRight(cfg.Source, "/")

			if err := os.MkdirAll(configHome(), 0o700); err != nil {
				return err
			}
			data, err := json.Marshal(cfg)
			if err != nil {
				return err
			}
			// Contains an API token: keep it owner-readable only.
			if err := os.WriteFile(pwpushConfigPath(), data, 0o600); err != nil {
				return err
			}
			// WriteFile's mode applies only on creation; enforce it too when a
			// pre-existing config file had broader permissions.
			if err := os.Chmod(pwpushConfigPath(), 0o600); err != nil {
				return err
			}
			ui.Printf("Saved auth config to %s", pwpushConfigPath())
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringVar(&cfg.Token, "token", "", "Your API token for Password Pusher.")
	flags.StringVar(&cfg.Source, "source", "", "Base URL of the instance, e.g. https://pwpush.com")
	flags.StringVar(&cfg.Email, "email", "",
		"Your email, for self-hosted legacy auth, e.g. user@example.com")
	_ = cmd.MarkFlagRequired("token")
	_ = cmd.MarkFlagRequired("source")
	return cmd
}

func newPwpushListActiveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list-active",
		Short: "List active passwords",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadPwpushConfig()
			if err != nil {
				return err
			}

			body, err := pwpushRequest(cmd.Context(), http.MethodGet, cfg, "/p/active.json", nil)
			if err != nil {
				return err
			}

			var pushes []struct {
				Note           string `json:"note"`
				URLToken       string `json:"url_token"`
				DaysRemaining  int    `json:"days_remaining"`
				ViewsRemaining int    `json:"views_remaining"`
			}
			if err := json.Unmarshal(body, &pushes); err != nil {
				return fmt.Errorf("could not parse response: %w", err)
			}

			table := &ui.Table{
				Headers: []ui.Cell{
					ui.Text("ID", ui.BoldCyan),
					ui.Text("Note", ui.BoldCyan),
					ui.Text("URL", ui.BoldCyan),
					ui.Text("Days Left", ui.BoldCyan),
					ui.Text("Views Left", ui.BoldCyan),
				},
				Aligns: []ui.Align{ui.AlignRight, ui.AlignLeft, ui.AlignLeft, ui.AlignRight, ui.AlignRight},
				Border: ui.Dim,
			}
			for i, push := range pushes {
				if push.DaysRemaining <= 0 {
					continue
				}
				table.Rows = append(table.Rows, []ui.Cell{
					ui.Text(strconv.Itoa(i), ui.Plain),
					ui.Text(push.Note, ui.Plain),
					ui.Text(cfg.Source+"/p/"+push.URLToken, ui.Cyan),
					ui.Text(strconv.Itoa(push.DaysRemaining), ui.Plain),
					ui.Text(strconv.Itoa(push.ViewsRemaining), ui.Plain),
				})
			}
			if len(table.Rows) == 0 {
				ui.Warn("No active passwords.")
				return nil
			}
			table.Render(ui.Err)
			return nil
		},
	}
}

func newPwpushSendCommand() *cobra.Command {
	var (
		days      int
		views     int
		note      string
		deletable bool
		file      string
		kind      string
	)

	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a password push and print its URL",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadPwpushConfig()
			if err != nil {
				return err
			}

			var payload string
			switch {
			case file != "":
				content, err := os.ReadFile(file)
				if err != nil {
					return err
				}
				payload = strings.TrimSpace(string(content))
			case !term.IsTerminal(int(os.Stdin.Fd())):
				content, err := io.ReadAll(os.Stdin)
				if err != nil {
					return err
				}
				payload = strings.TrimSpace(string(content))
			default:
				edited, err := captureFromEditor("payload.txt", "")
				if err != nil {
					return err
				}
				payload = strings.TrimSpace(edited)
			}
			if payload == "" {
				ui.Error("No content entered. Aborting.")
				return exitWith(1)
			}

			password := map[string]any{
				"payload":               payload,
				"expire_after_days":     days,
				"expire_after_views":    views,
				"retrievable_by_viewer": 1,
				"deletable_by_viewer":   boolToInt(deletable),
				"kind":                  kind,
			}
			if note != "" {
				password["note"] = note
			}
			body, err := json.Marshal(map[string]any{"password": password})
			if err != nil {
				return err
			}

			response, err := pwpushRequest(cmd.Context(), http.MethodPost, cfg, "/p.json", body)
			if err != nil {
				return err
			}

			var created struct {
				URLToken string `json:"url_token"`
				Password struct {
					URLToken string `json:"url_token"`
				} `json:"password"`
			}
			if err := json.Unmarshal(response, &created); err != nil {
				return fmt.Errorf("could not parse response: %w", err)
			}
			token := created.URLToken
			if token == "" {
				token = created.Password.URLToken
			}
			if token == "" {
				return fmt.Errorf("succeeded but no URL token was returned: %s", string(response))
			}
			fmt.Fprintln(ui.Out)
			fmt.Fprintln(ui.Out, ui.Blue.Render(cfg.Source+"/p/"+token))
			return nil
		},
	}

	flags := cmd.Flags()
	flags.IntVarP(&days, "days", "d", 7, "Expire after days.")
	flags.IntVarP(&views, "views", "v", 5, "Expire after views.")
	flags.StringVarP(&note, "note", "n", "", "Optional note.")
	// No --not-deletable: --deletable=false already says it.
	flags.BoolVar(&deletable, "deletable", true, "Allow the viewer to delete the push.")
	flags.StringVarP(&file, "file", "f", "",
		"File to upload instead of opening an editor or reading piped stdin.")
	flags.StringVarP(&kind, "kind", "k", "password", "Type: password, url, or qr.")
	return cmd
}

func newPwgenCommand() *cobra.Command {
	var (
		length      int
		noSymbols   bool
		noUppercase bool
		noLowercase bool
		noDigits    bool
	)

	cmd := &cobra.Command{
		Use:   "pwgen",
		Short: "Generate a random password",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const (
				upper   = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
				lower   = "abcdefghijklmnopqrstuvwxyz"
				digits  = "0123456789"
				symbols = "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"
			)

			var chars string
			if !noUppercase {
				chars += upper
			}
			if !noLowercase {
				chars += lower
			}
			if !noDigits {
				chars += digits
			}
			if !noSymbols {
				chars += symbols
			}
			if chars == "" {
				ui.Error("No character types selected.")
				return exitWith(1)
			}
			if length < 1 {
				ui.Error("--length must be at least 1.")
				return exitWith(1)
			}

			password := make([]byte, length)
			for i := range password {
				// Rejection-free uniform pick from a crypto source, matching
				// Python's secrets.choice.
				n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
				if err != nil {
					return err
				}
				password[i] = chars[n.Int64()]
			}
			fmt.Fprintln(ui.Out, string(password))
			return nil
		},
	}

	flags := cmd.Flags()
	flags.IntVarP(&length, "length", "l", 16, "Password length.")
	flags.BoolVar(&noSymbols, "no-symbols", false, "Exclude symbols.")
	flags.BoolVar(&noUppercase, "no-uppercase", false, "Exclude uppercase letters.")
	flags.BoolVar(&noLowercase, "no-lowercase", false, "Exclude lowercase letters.")
	flags.BoolVar(&noDigits, "no-digits", false, "Exclude digits.")
	return cmd
}

// pwpushRequest performs an authenticated API call and returns the response body.
func pwpushRequest(ctx context.Context, method string, cfg *pwpushConfig, path string, body []byte) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, cfg.Source+path, reader)
	if err != nil {
		return nil, err
	}
	for key, value := range cfg.headers() {
		req.Header.Set(key, value)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("error %d: %s", resp.StatusCode, strings.TrimSpace(string(content)))
	}
	return content, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
