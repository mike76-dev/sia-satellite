package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mike76-dev/sia-satellite/modules"

	"gitlab.com/NebulousLabs/go-upnp"
)

// myExternalIP discovers the gateway's external IP by querying a centralized
// service, http://myexternalip.com.
func myExternalIP() (_ string, err error) {
	// Timeout after 10 seconds.
	client := http.Client{Timeout: time.Duration(10 * time.Second)}
	resp, err := client.Get("http://myexternalip.com/raw")
	if err != nil {
		return "", err
	}
	defer func() {
		err = modules.ComposeErrors(err, resp.Body.Close())
	}()
	if resp.StatusCode != http.StatusOK {
		errResp, _ := ioutil.ReadAll(resp.Body)
		return "", errors.New(string(errResp))
	}
	buf, err := ioutil.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}
	if len(buf) == 0 {
		return "", errors.New("myexternalip.com returned a 0 length IP address")
	}
	// Trim newline.
	return strings.TrimSpace(string(buf)), nil
}

// managedIPFromUPNP attempts learn the Gateway's external IP address via UPnP.
func (g *Gateway) managedIPFromUPNP(ctx context.Context) (string, error) {
	d, err := upnp.Load(g.persist.RouterURL)
	if err != nil {
		d, err = upnp.DiscoverCtx(ctx)
		if err != nil {
			return "", err
		}
		loc := d.Location()
		g.mu.Lock()
		g.persist.RouterURL = loc
		if err = g.save(); err != nil {
			g.log.Println("WARN: could not save the gateway:", err)
		}
		g.mu.Unlock()
	}
	return d.ExternalIP()
}

// managedLearnHostname tries to discover the external ip of the machine. If
// discovering the address failed or if it is invalid, an error is returned.
func (g *Gateway) managedLearnHostname(cancel <-chan struct{}) (net.IP, error) {
	// Create ctx to cancel upnp discovery during shutdown.
	ctx, ctxCancel := context.WithTimeout(g.threads.StopCtx(), timeoutIPDiscovery)
	defer ctxCancel()
	go func() {
		select {
		case <-cancel:
			ctxCancel()
		case <-g.threads.StopChan():
			ctxCancel()
		case <-ctx.Done():
		}
	}()

	// Try UPnP first (unless disabled), then peer-to-peer discovery, then
	// myexternalip.com.
	var host string
	var err error
	if g.staticUseUPNP {
		host, err = g.managedIPFromUPNP(ctx)
	}
	if err != nil || host == "" {
		host, err = g.managedIPFromPeers(ctx.Done())
	}
	if err != nil || host == "" {
		host, err = myExternalIP()
	}
	if err != nil {
		return nil, modules.AddContext(err, "failed to discover external IP")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, fmt.Errorf("%v is not a valid IP", host)
	}
	return ip, nil
}

// threadedLearnHostname discovers the external IP of the Gateway regularly.
func (g *Gateway) threadedLearnHostname() {
	if err := g.threads.Add(); err != nil {
		return
	}
	defer g.threads.Done()

	for {
		host, err := g.managedLearnHostname(nil)
		if err != nil {
			g.log.Println("WARN: failed to discover external IP:", err)
		}
		// If we were unable to discover our IP we try again later.
		if err != nil {
			if !g.managedSleep(rediscoverIPIntervalFailure) {
				return // Shutdown interrupted sleep.
			}
			continue
		}

		g.mu.RLock()
		addr := modules.NetAddress(net.JoinHostPort(host.String(), g.port))
		g.mu.RUnlock()
		if err := addr.IsValid(); err != nil {
			g.log.Printf("WARN: discovered hostname %q is invalid: %v", addr, err)
			if !g.managedSleep(rediscoverIPIntervalFailure) {
				return // Shutdown interrupted sleep.
			}
			continue
		}

		g.mu.Lock()
		oldAddr := g.myAddr
		g.myAddr = addr
		g.mu.Unlock()

		if addr != oldAddr {
			g.log.Println("INFO: our address is", addr)
		}

		// Rediscover the IP later in case it changed.
		if !g.managedSleep(rediscoverIPIntervalSuccess) {
			return // Shutdown interrupted sleep.
		}
	}
}

// managedForwardPort adds a port mapping to the router.
func (g *Gateway) managedForwardPort(port string) error {
	if !g.staticUseUPNP {
		// UPnP is disabled.
		return nil
	}

	// If the port is invalid, there is no need to perform any of the other
	// tasks.
	portInt, err := strconv.Atoi(port)
	if err != nil {
		return err
	}

	// Create a context to stop UPnP discovery in case of a shutdown.
	ctx, cancel := context.WithCancel(g.threads.StopCtx())
	defer cancel()
	go func() {
		select {
		case <-g.threads.StopChan():
			cancel()
		case <-ctx.Done():
		}
	}()

	// Look for UPnP-enabled devices.
	d, err := upnp.DiscoverCtx(ctx)
	if err != nil {
		err = fmt.Errorf("WARN: could not automatically forward port %s: no UPnP-enabled devices found: %s", port, err)
		return err
	}

	// Forward port.
	err = d.ForwardTCP(uint16(portInt), "Satellite RPC")
	if err != nil {
		err = fmt.Errorf("WARN: could not automatically forward port %s: %s", port, err)
		return err
	}

	// Establish port-clearing at shutdown.
	g.threads.AfterStop(func() {
		g.managedClearPort(port)
	})

	return nil
}

// managedClearPort removes a port mapping from the router.
func (g *Gateway) managedClearPort(port string) {
	ctx, cancel := context.WithCancel(g.threads.StopCtx())
	defer cancel()
	go func() {
		select {
		case <-g.threads.StopChan():
			cancel()
		case <-ctx.Done():
		}
	}()
	d, err := upnp.DiscoverCtx(ctx)
	if err != nil {
		return
	}

	portInt, _ := strconv.Atoi(port)
	err = d.Clear(uint16(portInt))
	if err != nil {
		g.log.Printf("WARN: could not automatically unforward port %s: %s\n", port, err)
		return
	}

	g.log.Println("INFO: successfully unforwarded port", port)
}

// threadedForwardPort forwards a port and logs potential errors.
func (g *Gateway) threadedForwardPort(port string) {
	if err := g.threads.Add(); err != nil {
		return
	}
	defer g.threads.Done()

	if err := g.managedForwardPort(port); err != nil {
		return
	}
	g.log.Println("INFO: successfully forwarded port", port)
}
