/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package service

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/gorilla/websocket"
	"golang.org/x/net/proxy"
)

type bufferedProxyConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedProxyConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

// NewWebSocketDialerWithProxy builds a WebSocket dialer that follows the same
// channel proxy schemes accepted by the relay HTTP client.
func NewWebSocketDialerWithProxy(rawProxyURL string, handshakeTimeout time.Duration) (*websocket.Dialer, error) {
	dialer := *websocket.DefaultDialer
	if handshakeTimeout > 0 {
		dialer.HandshakeTimeout = handshakeTimeout
	}
	if common.TLSInsecureSkipVerify {
		dialer.TLSClientConfig = common.InsecureTLSConfig.Clone()
	}

	parsedURL, legacySuffixStripped, err := common.ParseProxyURLRuntime(rawProxyURL)
	if err != nil {
		return nil, err
	}
	if parsedURL == nil {
		return &dialer, nil
	}
	config := newProxyURLConfig(parsedURL)
	if legacySuffixStripped {
		warnLegacyProxyURLOnce(config)
	}

	dialer.Proxy = nil
	switch parsedURL.Scheme {
	case "socks5", "socks5h":
		forwardDialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		proxyDialer, err := proxy.FromURL(parsedURL, forwardDialer)
		if err != nil {
			return nil, err
		}
		contextDialer, ok := proxyDialer.(proxy.ContextDialer)
		if !ok {
			return nil, fmt.Errorf("SOCKS proxy dialer does not support context cancellation")
		}
		dialer.NetDialContext = contextDialer.DialContext
	case "http", "https":
		dialer.NetDialContext = func(ctx context.Context, network, targetAddress string) (net.Conn, error) {
			return dialHTTPProxyTunnel(ctx, network, targetAddress, parsedURL)
		}
	default:
		return nil, fmt.Errorf("unsupported proxy scheme")
	}
	return &dialer, nil
}

func dialHTTPProxyTunnel(ctx context.Context, network, targetAddress string, proxyURL *url.URL) (net.Conn, error) {
	proxyPort := proxyURL.Port()
	if proxyPort == "" {
		proxyPort = "80"
		if proxyURL.Scheme == "https" {
			proxyPort = "443"
		}
	}
	proxyAddress := net.JoinHostPort(proxyURL.Hostname(), proxyPort)
	forwardDialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	conn, err := forwardDialer.DialContext(ctx, network, proxyAddress)
	if err != nil {
		return nil, err
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = conn.Close()
		}
	}()

	if proxyURL.Scheme == "https" {
		tlsConfig := &tls.Config{ServerName: proxyURL.Hostname()}
		if common.TLSInsecureSkipVerify {
			tlsConfig = common.InsecureTLSConfig.Clone()
			tlsConfig.ServerName = proxyURL.Hostname()
		}
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		conn = tlsConn
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}

	header := make(http.Header)
	if proxyURL.User != nil {
		password, _ := proxyURL.User.Password()
		credentials := proxyURL.User.Username() + ":" + password
		header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(credentials)))
	}
	connectRequest := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: targetAddress},
		Host:   targetAddress,
		Header: header,
	}
	if err := connectRequest.Write(conn); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, connectRequest)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy CONNECT failed: %s", response.Status)
	}

	closeOnError = false
	if reader.Buffered() == 0 {
		return conn, nil
	}
	return &bufferedProxyConn{Conn: conn, reader: reader}, nil
}
