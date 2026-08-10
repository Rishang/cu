package cli

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestBastionEndpointNoBastion pins the common case: a profile with no
// bastion configured passes its endpoint through untouched, and never dials
// anything.
func TestBastionEndpointNoBastion(t *testing.T) {
	p := secretProvider{Profile: "prod", Endpoint: "https://vault.example.com"}
	endpoint, serverName, err := bastionEndpoint(context.Background(), p)
	if err != nil || endpoint != p.Endpoint || serverName != "" {
		t.Fatalf("bastionEndpoint() = %q, %q, %v; want %q, \"\", nil", endpoint, serverName, err, p.Endpoint)
	}
}

// TestDialBastion exercises the real SSH protocol against an in-process
// server: it accepts a direct-tcpip channel the same way a real bastion does
// and forwards it to a local echo listener, so a byte round-trip through
// dialBastion's tunnel pins both the handshake and the forwarding loop.
func TestDialBastion(t *testing.T) {
	echoAddr := startEchoServer(t)
	bastionAddr, clientKeyPath := startTestBastion(t)
	bastionHost, bastionPort := splitHostPort(t, bastionAddr)

	p := secretProvider{Profile: "test"}
	p.Bastion.Host = bastionHost
	p.Bastion.Port = bastionPort
	p.Bastion.User = "test"
	p.Bastion.Key = clientKeyPath

	tunnel, err := dialBastion(context.Background(), p, echoAddr)
	if err != nil {
		t.Fatalf("dialBastion: %v", err)
	}

	conn, err := net.Dial("tcp", tunnel.Addr())
	if err != nil {
		t.Fatalf("dialing tunnel: %v", err)
	}
	defer conn.Close()

	want := []byte("hello through the bastion")
	if _, err := conn.Write(want); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("reading echo: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("echoed %q, want %q", got, want)
	}
}

// startEchoServer starts a TCP listener that echoes back whatever it reads,
// standing in for the "remote" service a bastion forwards to.
func startEchoServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()
	return listener.Addr().String()
}

// startTestBastion starts an in-process SSH server that accepts only the
// generated test client key and forwards direct-tcpip channels, the minimum
// a real bastion needs to present for dialBastion to work against it. It
// returns the server's address and the path to the client's private key.
func startTestBastion(t *testing.T) (addr, clientKeyPath string) {
	t.Helper()

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatal(err)
	}

	clientPub, clientPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientPublicKey, err := ssh.NewPublicKey(clientPub)
	if err != nil {
		t.Fatal(err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(clientPriv)
	if err != nil {
		t.Fatal(err)
	}
	clientKeyPath = filepath.Join(t.TempDir(), "id_ed25519")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(clientKeyPath, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) != string(clientPublicKey.Marshal()) {
				return nil, fmt.Errorf("unrecognized key")
			}
			return nil, nil
		},
	}
	config.AddHostKey(hostSigner)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go serveTestBastionConn(conn, config)
		}
	}()

	return listener.Addr().String(), clientKeyPath
}

// serveTestBastionConn runs one SSH server connection, forwarding every
// direct-tcpip channel it's asked to open — the server side of the same
// port-forward protocol dialBastion drives from the client.
func serveTestBastionConn(conn net.Conn, config *ssh.ServerConfig) {
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "direct-tcpip" {
			newChannel.Reject(ssh.UnknownChannelType, "only direct-tcpip supported")
			continue
		}
		var target struct {
			Host       string
			Port       uint32
			OriginHost string
			OriginPort uint32
		}
		if err := ssh.Unmarshal(newChannel.ExtraData(), &target); err != nil {
			newChannel.Reject(ssh.ConnectionFailed, "bad direct-tcpip request")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go ssh.DiscardRequests(requests)
		go forwardTestChannel(channel, net.JoinHostPort(target.Host, strconv.Itoa(int(target.Port))))
	}
}

func forwardTestChannel(channel ssh.Channel, addr string) {
	defer channel.Close()
	remote, err := net.Dial("tcp", addr)
	if err != nil {
		return
	}
	defer remote.Close()
	go io.Copy(remote, channel)
	io.Copy(channel, remote)
}

func splitHostPort(t *testing.T, addr string) (host string, port int) {
	t.Helper()
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err = strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return h, port
}
