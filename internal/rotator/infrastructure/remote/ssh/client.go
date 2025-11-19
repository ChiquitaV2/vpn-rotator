package ssh

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"

	apperrors "github.com/chiquitav2/vpn-rotator/pkg/errors"
	applogger "github.com/chiquitav2/vpn-rotator/pkg/logger"
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
	mutex  sync.Mutex
	logger *applogger.Logger
}

// NewClient creates a new SSH client
func NewClient(host, user, privateKey string, logger *applogger.Logger) (Client, error) {
	signer, err := ssh.ParsePrivateKey([]byte(privateKey))
	if err != nil {
		return nil, apperrors.NewSystemError(
			apperrors.ErrCodeConfiguration,
			"failed to parse private key",
			false, err,
		)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	return &client{
		config: config,
		host:   host,
		logger: logger.WithComponent("ssh.client").With(slog.String("host", host)),
	}, nil
}

// RunCommand executes a command on the remote host
func (c *client) RunCommand(ctx context.Context, command string) (string, error) {
	op := c.logger.StartOp(ctx, "run_command", slog.String("command", command))

	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.conn == nil {
		if err := c.connect(ctx); err != nil {
			op.Fail(err, "connection failed")
			return "", err
		}
	}

	session, err := c.conn.NewSession()
	if err != nil {
		c.logger.DebugContext(ctx, "SSH session failed, attempting reconnect", slog.String("error", err.Error()))
		if err := c.reconnect(ctx); err != nil {
			op.Fail(err, "reconnect failed")
			return "", err
		}
		session, err = c.conn.NewSession()
		if err != nil {
			err = apperrors.NewInfrastructureError(apperrors.ErrCodeSSHConnection, "failed to create session after reconnect", true, err)
			op.Fail(err, "session creation failed after reconnect")
			return "", err
		}
	}
	defer session.Close()

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			session.Signal(ssh.SIGKILL)
		case <-done:
		}
	}()

	output, err := session.CombinedOutput(command)
	close(done)

	if err != nil {
		err = apperrors.NewInfrastructureError(apperrors.ErrCodeSSHCommand, "ssh command failed", true, err)
		op.Fail(err, "command execution failed", slog.String("output", string(output)))
		return string(output), err
	}

	op.Complete("command executed successfully")
	return string(output), nil
}

// connect establishes a new SSH connection (not thread-safe, caller must hold lock)
func (c *client) connect(ctx context.Context) error {
	conn, err := ssh.Dial("tcp", net.JoinHostPort(c.host, "22"), c.config)
	if err != nil {
		return apperrors.NewInfrastructureError(apperrors.ErrCodeSSHConnection, "failed to connect to host", true, err).
			WithMetadata("host", c.host)
	}
	c.conn = conn
	return nil
}

// reconnect closes the existing connection and establishes a new one
func (c *client) reconnect(ctx context.Context) error {
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	return c.connect(ctx)
}

// Close closes the SSH connection
func (c *client) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		if err != nil {
			c.logger.WarnContext(context.Background(), "error closing ssh connection", slog.String("error", err.Error()))
		}
		return err
	}
	return nil
}

// IsHealthy checks if the SSH connection is healthy
func (c *client) IsHealthy() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.conn == nil {
		return false
	}

	session, err := c.conn.NewSession()
	if err != nil {
		c.logger.DebugContext(context.Background(), "health check failed: session creation", slog.String("error", err.Error()))
		return false
	}
	session.Close()
	return true
}
