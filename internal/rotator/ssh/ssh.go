package ssh

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Client defines the interface for an SSH client.
type Client interface {
	RunCommand(ctx context.Context, command string) (string, error)
}

type client struct {
	config *ssh.ClientConfig
	host   string
}

// NewClient creates a new SSH client.
func NewClient(host, user, privateKey string) (Client, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: In a real app, use a proper host key callback.
	}

	return &client{
		config: config,
		host:   fmt.Sprintf("%s:22", host),
	}, nil
}

func (c *client) RunCommand(ctx context.Context, command string) (string, error) {
	c.config.Timeout = 10 * time.Second

	client, err := ssh.Dial("tcp", c.host, c.config)
	if err != nil {
		return "", fmt.Errorf("failed to dial: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	var stdoutBuf bytes.Buffer
	session.Stdout = &stdoutBuf

	errCh := make(chan error, 1)
	go func() {
		errCh <- session.Run(command)
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case err := <-errCh:
		if err != nil {
			return "", fmt.Errorf("failed to run command: %w", err)
		}
	}

	return strings.TrimSpace(stdoutBuf.String()), nil
}
