package ssh

import (
	"context"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// Client defines the interface for SSH operations
type Client interface {
	RunCommand(ctx context.Context, command string) (string, error)
	Close() error
	IsHealthy() bool
}

// client implements the Client interface using golang.org/x/crypto/ssh
type client struct {
	config *ssh.ClientConfig
	host   string
	conn   *ssh.Client
}

// NewClient creates a new SSH client
func NewClient(host, user, privateKey string) (Client, error) {
	// Parse the private key
	signer, err := ssh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// Create SSH client configuration
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: Implement proper host key verification
		Timeout:         30 * time.Second,
	}

	return &client{
		config: config,
		host:   host,
	}, nil
}

// RunCommand executes a command on the remote host
func (c *client) RunCommand(ctx context.Context, command string) (string, error) {
	// Establish connection if not already connected
	if c.conn == nil {
		conn, err := ssh.Dial("tcp", net.JoinHostPort(c.host, "22"), c.config)
		if err != nil {
			return "", fmt.Errorf("failed to connect to %s: %w", c.host, err)
		}
		c.conn = conn
	}

	// Create a session
	session, err := c.conn.NewSession()
	if err != nil {
		// Connection might be stale, try to reconnect
		c.conn.Close()
		c.conn = nil

		conn, err := ssh.Dial("tcp", net.JoinHostPort(c.host, "22"), c.config)
		if err != nil {
			return "", fmt.Errorf("failed to reconnect to %s: %w", c.host, err)
		}
		c.conn = conn

		session, err = c.conn.NewSession()
		if err != nil {
			return "", fmt.Errorf("failed to create session: %w", err)
		}
	}
	defer session.Close()

	// Set up context cancellation
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			session.Signal(ssh.SIGKILL)
		case <-done:
		}
	}()

	// Execute the command
	output, err := session.CombinedOutput(command)
	close(done)

	if err != nil {
		return string(output), fmt.Errorf("command failed: %w", err)
	}

	return string(output), nil
}

// Close closes the SSH connection
func (c *client) Close() error {
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

// IsHealthy checks if the SSH connection is healthy
func (c *client) IsHealthy() bool {
	if c.conn == nil {
		return false
	}

	// Try to create a session to test the connection
	session, err := c.conn.NewSession()
	if err != nil {
		return false
	}
	session.Close()
	return true
}
