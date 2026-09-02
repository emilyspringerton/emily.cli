// cmd/email.go — emily email send
//
// A real, general-purpose "send one plain-text email" CLI command, extracted from a one-off Go
// program written in-session (2026-09-02, founder real-time: "put the credentials somewhere and
// keep it moving," then "can we make sending email a tool in the emily cli?") to deliver a real
// PAPERCRAFT/IDUNA account's login credentials to garybifrost@gmail.com. That specific send
// failed from this Claude Code sandbox — outbound SMTP (587 AND 465) both time out here even
// though plain HTTPS works fine, a real, confirmed egress restriction, not a code/credential bug
// (see EMILY/var/pending-credentials-gary-papercraft.env for the real, undelivered credentials
// this same session generated). This command exists so that same send can be retried from any
// environment that DOES have SMTP egress (the founder's own machine, a production host) without
// hand-writing throwaway Go each time.
//
// Reuses the exact SMTP path (STARTTLS on :587, Gmail App Password auth) emily-agent/gmail.go's
// own sendViaSMTP already established as "Path 2" -- same real credentials
// (GMAIL_SMTP_ADDRESS/GMAIL_SMTP_PASSWORD, `emily key set` into EMILY/var/emily-secrets.env),
// now auto-resolved by internal/config.Resolve() the same way ANTHROPIC_API_KEY already is.
// Deliberately does NOT touch the OAuth2/Gmail-API "Path 1" that file also supports (real inbox
// reading needs that path specifically; a one-off CLI send has no such need) -- SMTP alone is
// sufficient and matches what's actually configured on this box today.
package cmd

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"os"

	"github.com/emilyspringerton/emily-cli/internal/config"
)

const (
	emailSMTPHost = "smtp.gmail.com"
	emailSMTPPort = "587" // STARTTLS, not the 465 implicit-TLS port -- matches gmail.go's own choice
)

func emailUsage() int {
	fmt.Fprintln(os.Stderr, `usage:
  emily email send --to <address> --subject <text> (--body <text> | --body-file <path>)

Sends one plain-text email via Gmail SMTP (STARTTLS + App Password auth), using
GMAIL_SMTP_ADDRESS/GMAIL_SMTP_PASSWORD from the environment or EMILY/var/emily-secrets.env
(set with: emily key set GMAIL_SMTP_ADDRESS <address> / emily key set GMAIL_SMTP_PASSWORD <app-password>).

Requires a Google Account App Password (Google Account -> Security -> 2-Step Verification ->
App Passwords, needs 2FA enabled on that account) -- not the account's real login password.`)
	return 1
}

// RunEmail implements `emily email send`.
func RunEmail(args []string) int {
	if len(args) == 0 || args[0] != "send" {
		return emailUsage()
	}
	rest := args[1:]

	var to, subject, body, bodyFile string
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--to":
			i++
			if i < len(rest) {
				to = rest[i]
			}
		case "--subject":
			i++
			if i < len(rest) {
				subject = rest[i]
			}
		case "--body":
			i++
			if i < len(rest) {
				body = rest[i]
			}
		case "--body-file":
			i++
			if i < len(rest) {
				bodyFile = rest[i]
			}
		default:
			fmt.Fprintf(os.Stderr, "emily email send: unknown flag %q\n", rest[i])
			return emailUsage()
		}
	}

	if to == "" || subject == "" || (body == "" && bodyFile == "") {
		fmt.Fprintln(os.Stderr, "emily email send: --to, --subject, and one of --body/--body-file are all required")
		return emailUsage()
	}
	if bodyFile != "" {
		data, err := os.ReadFile(bodyFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "emily email send: read --body-file: %v\n", err)
			return 1
		}
		body = string(data)
	}

	cfg, err := config.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "emily email send: resolve config: %v\n", err)
		return 1
	}
	if cfg.GmailSMTPAddress == "" || cfg.GmailSMTPPassword == "" {
		fmt.Fprintln(os.Stderr, "emily email send: GMAIL_SMTP_ADDRESS/GMAIL_SMTP_PASSWORD not configured -- "+
			"set them with 'emily key set GMAIL_SMTP_ADDRESS <address>' and 'emily key set GMAIL_SMTP_PASSWORD <app-password>'")
		return 1
	}

	if err := sendPlainTextEmail(cfg.GmailSMTPAddress, cfg.GmailSMTPPassword, to, subject, body); err != nil {
		fmt.Fprintf(os.Stderr, "emily email send: %v\n", err)
		return 1
	}
	fmt.Printf("Sent to %s.\n", to)
	return 0
}

// sendPlainTextEmail sends one plain-text message through Gmail's real SMTP submission
// endpoint using STARTTLS + AUTH PLAIN -- the exact same real mechanism emily-agent/gmail.go's
// own sendViaSMTP uses, duplicated here rather than imported since gmail.go lives in
// emily-agent's own `package main`, not a shared library package.
func sendPlainTextEmail(fromAddr, appPassword, to, subject, body string) error {
	auth := smtp.PlainAuth("", fromAddr, appPassword, emailSMTPHost)
	raw := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		fromAddr, to, subject, body)

	client, err := smtp.Dial(emailSMTPHost + ":" + emailSMTPPort)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer client.Close()

	if err := client.StartTLS(&tls.Config{ServerName: emailSMTPHost}); err != nil {
		return fmt.Errorf("smtp starttls: %w", err)
	}
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(fromAddr); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write([]byte(raw)); err != nil {
		return fmt.Errorf("smtp write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close body: %w", err)
	}
	return client.Quit()
}
