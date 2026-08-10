package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"

	"golang.org/x/crypto/ssh"
)

// bastionTunnel is a live SSH port-forward opened for one profile: every
// connection accepted on its local listener is forwarded, through the
// bastion, to the address it was opened for.
//
// It is never closed explicitly. cu is a one-shot process per command, so the
// listener and SSH connection die with it; a long-running daemon reusing this
// would need to track and close them instead.
type bastionTunnel struct {
	listener net.Listener
	client   *ssh.Client
}

// Addr is the local address a client should connect to instead of the real
// endpoint.
func (t *bastionTunnel) Addr() string { return t.listener.Addr().String() }

// dialBastion opens an SSH connection to the profile's bastion and starts a
// local listener that forwards each accepted connection to remoteAddr through
// it. b.Host == "" is not this function's contract to check; callers use
// bastionEndpoint for the common "no bastion configured" case.
func dialBastion(ctx context.Context, p secretProvider, remoteAddr string) (*bastionTunnel, error) {
	b := p.Bastion
	if b.User == "" || b.Key == "" {
		return nil, fmt.Errorf("profile %q needs bastion.user and bastion.key", p.Profile)
	}
	keyPEM, err := os.ReadFile(b.Key)
	if err != nil {
		return nil, fmt.Errorf("reading bastion.key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(keyPEM)
	if err != nil {
		return nil, fmt.Errorf("parsing bastion.key: %w", err)
	}

	port := b.Port
	if port == 0 {
		port = 22
	}
	config := &ssh.ClientConfig{
		User: b.User,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		// ponytail: no host-key verification, so a MITM on the bastion hop
		// goes unnoticed. Add golang.org/x/crypto/ssh/knownhosts if that hop
		// isn't already a trusted network.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(b.Host, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("dialing bastion %s: %w", b.Host, err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, conn.RemoteAddr().String(), config)
	if err != nil {
		return nil, fmt.Errorf("bastion handshake with %s: %w", b.Host, err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		client.Close()
		return nil, err
	}
	go acceptAndForward(listener, client, remoteAddr)
	return &bastionTunnel{listener: listener, client: client}, nil
}

// acceptAndForward pairs every connection the local listener accepts with a
// channel dialed through the bastion to remoteAddr, until the listener closes.
func acceptAndForward(listener net.Listener, client *ssh.Client, remoteAddr string) {
	for {
		local, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer local.Close()
			remote, err := client.Dial("tcp", remoteAddr)
			if err != nil {
				return
			}
			defer remote.Close()
			go io.Copy(remote, local)
			io.Copy(local, remote)
		}()
	}
}

// bastionEndpoint rewrites a profile's endpoint to route through its bastion
// when one is configured, opening the tunnel as a side effect. It returns the
// endpoint unchanged and an empty serverName — the common case — when
// p.Bastion.Host is empty.
//
// serverName is the endpoint's original hostname, needed because the client
// now dials a local address but the server's certificate is still issued for
// the real one.
func bastionEndpoint(ctx context.Context, p secretProvider) (endpoint, serverName string, err error) {
	if p.Bastion.Host == "" {
		return p.Endpoint, "", nil
	}

	target, err := url.Parse(p.Endpoint)
	if err != nil {
		return "", "", fmt.Errorf("profile %q has an invalid endpoint: %w", p.Profile, err)
	}
	remoteHost, remotePort := target.Hostname(), target.Port()
	if remotePort == "" {
		remotePort = defaultPort(target.Scheme)
	}

	tunnel, err := dialBastion(ctx, p, net.JoinHostPort(remoteHost, remotePort))
	if err != nil {
		return "", "", err
	}
	target.Host = tunnel.Addr()
	return target.String(), remoteHost, nil
}

// defaultPort is the port a scheme talks on when the endpoint's URL omits one.
func defaultPort(scheme string) string {
	if scheme == "http" {
		return "80"
	}
	return "443"
}
