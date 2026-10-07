// Copyright 2026 Lucas Saavedra Vaz
// Portions Copyright (c) 2025 DeedleFake, MIT License; see LICENSES/MIT-Trayscale.txt
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package ui is the GTK application. Its structure follows Trayscale's
// internal/ui.
package ui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	traygolin "github.com/lucasssvaz/traygolin"
	"github.com/lucasssvaz/traygolin/internal/metadata"
	"github.com/lucasssvaz/traygolin/internal/olm"
	"github.com/lucasssvaz/traygolin/internal/pangolin"
	"github.com/lucasssvaz/traygolin/internal/poller"
	"github.com/lucasssvaz/traygolin/internal/privhelper"
	"github.com/lucasssvaz/traygolin/internal/schema"
	"github.com/lucasssvaz/traygolin/internal/tray"
)

const (
	connectTimeout    = 45 * time.Second
	disconnectTimeout = 15 * time.Second
)

// App is the main type for the app, containing all of the state
// necessary to run it.
type App struct {
	ctx    context.Context
	poller *poller.Poller
	cli    *pangolin.Client
	olm    *olm.Client

	app      *adw.Application
	win      *MainWindow
	prefs    *PreferencesDialog
	settings *gio.Settings
	tray     *tray.Tray

	iconPath string

	state        State
	online       bool
	seenTunnel   bool
	autoConnect  bool
	pollOverride time.Duration
	actions      map[string]*gio.SimpleAction
	quitOnce     sync.Once
}

func (a *App) clip(text string) {
	gdk.DisplayGetDefault().Clipboard().SetText(text)
}

func (a *App) copyText(text, toast string) {
	if text == "" {
		return
	}
	a.clip(text)
	a.toast(toast)
}

func (a *App) notify(title, body string) {
	n := gio.NewNotification(title)
	n.SetBody(body)
	if icon := notificationIcon(a.iconPath); icon != nil {
		n.SetIcon(icon)
	}

	a.app.SendNotification("pangolin-status", n)
}

func (a *App) toast(msg string) {
	if a.win != nil {
		a.win.Toast(msg)
		return
	}
	a.notify(metadata.AppName, msg)
}

// setBusy marks a connect or disconnect in progress. An empty label
// clears it.
func (a *App) setBusy(label string) {
	a.state.Busy = label
	a.tray.SetBusy(label)
	a.syncActions()
	if a.win != nil {
		a.win.Update(&a.state)
	}
}

func (a *App) update(status poller.Status) {
	switch status := status.(type) {
	case *poller.TunnelStatus:
		a.state.Tunnel = status
		online := status.Online()
		if a.seenTunnel && a.online != online {
			body := "Pangolin is disconnected."
			if online {
				body = "Pangolin is connected."
			}
			a.notify("Pangolin Status", body)
		}
		a.online = online
		a.seenTunnel = true

	case *poller.AccountStatus:
		a.state.Accounts = status

	case *poller.ServerStatus:
		a.state.Server = status

	case *poller.CLIStatus:
		a.state.CLI = status
	}

	a.syncActions()
	a.tray.Update(status)
	if a.win != nil {
		a.win.Update(&a.state)
	}
	if a.prefs != nil {
		a.prefs.Update(&a.state)
	}
	a.maybeConnectAtStart()
}

func (a *App) init(ctx context.Context) {
	if _, err := schema.Prepare(traygolin.SchemaXML); err != nil {
		slog.Warn("compile gsettings schema", "err", err)
	} else {
		a.settings = gio.NewSettings(metadata.AppID)
	}

	a.app = adw.NewApplication(metadata.AppID, gio.ApplicationFlagsNone)

	var hideWindow bool
	a.app.AddMainOption("hide-window", 0, glib.OptionFlagNone, glib.OptionArgNone, "Start in the tray without showing the window", "")
	a.app.AddMainOption("poll-interval", 0, glib.OptionFlagNone, glib.OptionArgInt, "Override the status poll interval in seconds", "SECONDS")
	a.app.ConnectHandleLocalOptions(func(options *glib.VariantDict) int {
		if options.Contains("hide-window") {
			hideWindow = true
		}
		if v := options.LookupValue("poll-interval", glib.NewVariantType("i")); v != nil {
			a.pollOverride = poller.ClampInterval(time.Duration(v.Int32()) * time.Second)
		}
		return -1
	})

	a.app.ConnectStartup(func() {
		a.app.Hold()
		a.iconPath = prepareIcon()
		a.initActions(ctx)
		a.initSettings(ctx)
	})

	a.app.ConnectActivate(func() {
		if hideWindow {
			hideWindow = false
			return
		}
		a.onAppActivate(ctx)
	})

	a.app.ConnectShutdown(func() {
		a.tray.Close()
	})
}

func (a *App) addAction(name string, f func()) *gio.SimpleAction {
	act := gio.NewSimpleAction(name, nil)
	act.ConnectActivate(func(*glib.Variant) { f() })
	a.app.AddAction(act)
	a.actions[name] = act
	return act
}

// addChoiceAction adds a string-stateful action, which menus show as a
// radio group whose state is the current choice.
func (a *App) addChoiceAction(name string, f func(string)) *gio.SimpleAction {
	act := gio.NewSimpleActionStateful(name, glib.NewVariantType("s"), glib.NewVariantString(""))
	act.ConnectActivate(func(v *glib.Variant) {
		if v != nil {
			f(v.String())
		}
	})
	a.app.AddAction(act)
	a.actions[name] = act
	return act
}

func setChoiceState(act *gio.SimpleAction, val string) {
	if act != nil && act.State().String() != val {
		act.SetState(glib.NewVariantString(val))
	}
}

func (a *App) initActions(ctx context.Context) {
	a.actions = make(map[string]*gio.SimpleAction)

	a.addAction("connect", a.startTunnel)
	a.addAction("disconnect", a.stopTunnel)
	a.addAction("login", a.showLogin)
	a.addAction("logout", a.logout)
	a.addChoiceAction("select-org", a.selectOrg)
	a.addChoiceAction("select-exit-node", a.selectExitNode)
	a.addChoiceAction("exit-nodes-unavailable", func(string) {}).SetEnabled(false)
	a.addAction("setup", a.showSetup)
	a.addAction("preferences", a.showPreferences)
	a.addAction("logs", a.showLogs)
	a.addAction("check-updates", a.checkUpdates)
	a.addAction("reset-dns", a.resetDNS)
	a.addAction("about", a.showAbout)
	a.addAction("copy-cli-install", func() { a.copyText(metadata.CLIInstall, "Copied install command") })
	a.addAction("quit", a.Quit)
	a.app.SetAccelsForAction("app.quit", []string{"<Ctrl>q"})
	a.app.SetAccelsForAction("app.preferences", []string{"<Ctrl>comma"})

	a.syncActions()
}

// syncActions enables only the actions that make sense right now.
func (a *App) syncActions() {
	if a.actions == nil {
		return
	}
	st := &a.state
	idle := st.Busy == ""
	cliOK := st.CLI == nil || st.CLI.Found()
	loggedIn := st.Accounts != nil && st.Accounts.Auth.LoggedIn
	running := st.Tunnel != nil && st.Tunnel.Running

	set := func(name string, on bool) {
		if act := a.actions[name]; act != nil {
			act.SetEnabled(on)
		}
	}
	set("connect", idle && cliOK && !running)
	set("disconnect", idle && running)
	set("login", idle && cliOK)
	set("logout", idle && cliOK && loggedIn)
	set("select-org", idle && cliOK && loggedIn)
	set("select-exit-node", idle && cliOK && loggedIn && tray.ExitNodesUnavailable(*st) == "")
	setChoiceState(a.actions["select-org"], tray.CurrentOrg(*st))
	setChoiceState(a.actions["select-exit-node"], CurrentExitNiceID(st))
	set("setup", st.CLI == nil || !st.CLI.HelperInstalled)
	set("check-updates", cliOK)
	set("reset-dns", idle && cliOK && !running)
}

func (a *App) onAppActivate(ctx context.Context) {
	if a.win != nil {
		a.win.MainWindow.Present()
		return
	}

	a.win = NewMainWindow(a)
	a.win.MainWindow.ConnectCloseRequest(func() bool {
		a.win = nil
		return false
	})

	a.win.Update(&a.state)
	a.win.MainWindow.Present()
}

func (a *App) initTray(ctx context.Context) {
	if a.tray != nil {
		return
	}

	t := &tray.Tray{
		Icons: tray.Icons{
			Inactive: traygolin.TrayIconPNG,
			Active:   traygolin.TrayConnectedPNG,
			ExitNode: traygolin.TrayExitPNG,
		},
		OnShow:       a.idle(func() { a.app.Activate() }),
		OnConnToggle: a.idle(a.toggleTunnel),
		OnLogin:      a.idle(a.showLoginWindow),
		OnLogout:     a.idle(a.logout),
		OnSelectAccount: func(acc pangolin.Account) {
			glib.IdleAdd(func() { a.selectAccount(acc) })
		},
		OnSelectOrg: func(id string) {
			glib.IdleAdd(func() { a.selectOrg(id) })
		},
		OnSelectExit: func(niceID string) {
			glib.IdleAdd(func() { a.selectExitNode(niceID) })
		},
		OnPreferences: a.idle(a.withWindow(a.showPreferences)),
		OnQuit:        a.Quit,
	}
	a.tray = t

	go func() {
		if err := t.Start(); err != nil {
			slog.Error("failed to start tray icon", "err", err)
			return
		}
		glib.IdleAdd(func() {
			if a.tray != t {
				return
			}
			t.SetBusy(a.state.Busy)
			if s := a.state.Tunnel; s != nil {
				t.Update(s)
			}
			if s := a.state.Accounts; s != nil {
				t.Update(s)
			}
			if s := a.state.Server; s != nil {
				t.Update(s)
			}
			if s := a.state.CLI; s != nil {
				t.Update(s)
			}
		})
	}()
}

func (a *App) idle(f func()) func() {
	return func() { glib.IdleAdd(f) }
}

// withWindow shows the main window before f so dialogs have a parent.
func (a *App) withWindow(f func()) func() {
	return func() {
		a.onAppActivate(a.ctx)
		f()
	}
}

func (a *App) showLoginWindow() {
	a.onAppActivate(a.ctx)
	a.showLogin()
}

func (a *App) toggleTunnel() {
	if a.state.Tunnel != nil && a.state.Tunnel.Running {
		a.stopTunnel()
		return
	}
	a.startTunnel()
}

func (a *App) upOptions() pangolin.UpOptions {
	opts := pangolin.UpOptions{Holepunch: true, MTU: 1280}
	if a.settings != nil {
		opts.Holepunch = a.settings.Boolean("holepunch")
		opts.DisableRelay = a.settings.Boolean("disable-relay")
		opts.MTU = int(a.settings.Int("mtu"))
	}
	return opts
}

func (a *App) resetSwitch() {
	if a.win != nil {
		a.win.Update(&a.state)
	}
}

func (a *App) startTunnel() {
	if a.state.Busy != "" {
		return
	}
	if a.state.Tunnel != nil && a.state.Tunnel.Running {
		return
	}

	cli, setup, login := OfflineNeeds(&a.state)
	switch {
	case cli:
		a.resetSwitch()
		a.toast("Install the Pangolin CLI first")
		return
	case setup:
		a.resetSwitch()
		a.onAppActivate(a.ctx)
		a.showSetup()
		return
	case login:
		a.resetSwitch()
		a.onAppActivate(a.ctx)
		Confirmation{
			Heading: "Login Required",
			Body:    "Log in to Pangolin before connecting?",
			Accept:  "_Log In",
			Reject:  "_Cancel",
		}.Show(a, func(accept bool) {
			if accept {
				a.showLogin()
			}
		})
		return
	}

	a.setBusy("Connecting…")
	opts := a.upOptions()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
		defer cancel()

		err := a.cli.Up(ctx, opts)
		if err == nil {
			err = a.waitTunnel(ctx, connectTimeout, func(s *poller.TunnelStatus) (bool, error) {
				if s.Online() {
					return true, nil
				}
				if s.Status != nil && s.Status.Error != nil && s.Status.Error.Message != "" {
					return true, errors.New(s.Status.Error.Message)
				}
				if s.Status != nil && s.Status.Terminated {
					return true, errors.New("the tunnel stopped while connecting")
				}
				return false, nil
			})
		}

		glib.IdleAdd(func() {
			a.setBusy("")
			if err != nil {
				a.connectFailed(err)
			}
		})
	}()
}

func (a *App) connectFailed(err error) {
	slog.Error("connect", "err", err)
	switch {
	case errors.Is(err, pangolin.ErrHelperMissing):
		a.onAppActivate(a.ctx)
		a.showSetup()
	case errors.Is(err, pangolin.ErrNotAuthorized):
		a.toast("Authorization was denied")
	default:
		msg := firstLine(err.Error())
		a.notify("Connection Failed", msg)
		if a.win != nil {
			a.win.Toast("Connection failed: " + msg)
		}
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

// waitTunnel polls quickly until done reports true or timeout passes.
func (a *App) waitTunnel(ctx context.Context, timeout time.Duration, done func(*poller.TunnelStatus) (bool, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case a.poller.Fast() <- timeout:
	case <-ctx.Done():
		return ctx.Err()
	}
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return errors.New("timed out waiting for the tunnel; see View Logs for details")
			}
			return ctx.Err()
		case s := <-a.poller.NextTunnel():
			if ok, err := done(s); ok {
				return err
			}
		}
	}
}

func (a *App) pollNow(ctx context.Context) {
	select {
	case <-a.poller.Poll():
	case <-ctx.Done():
	}
}

func (a *App) stopTunnel() {
	if a.state.Busy != "" {
		return
	}

	a.setBusy("Disconnecting…")
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, disconnectTimeout)
		defer cancel()

		err := a.olm.Exit(ctx)
		switch {
		case errors.Is(err, olm.ErrNotRunning):
			err = nil
		case errors.Is(err, olm.ErrPermission):
			err = privhelper.PrivilegedDown(ctx)
		case err == nil:
			err = a.olm.WaitStopped(ctx)
		}
		a.pollNow(ctx)

		glib.IdleAdd(func() {
			a.setBusy("")
			if err != nil {
				slog.Error("disconnect", "err", err)
				a.toast("Disconnect failed: " + firstLine(err.Error()))
			}
		})
	}()
}

// run runs f off the main thread and then refreshes state.
func (a *App) run(f func(context.Context) error, ok string) {
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
		defer cancel()

		err := f(ctx)
		a.pollNow(ctx)
		glib.IdleAdd(func() {
			if err != nil {
				slog.Error("action failed", "err", err)
				a.toast(firstLine(err.Error()))
				return
			}
			if ok != "" {
				a.toast(ok)
			}
		})
	}()
}

func (a *App) logout() {
	Confirmation{
		Heading:     "Log Out?",
		Body:        "This ends the active Pangolin session on this device. The tunnel will disconnect.",
		Accept:      "_Log Out",
		Reject:      "_Cancel",
		Destructive: true,
	}.Show(a, func(accept bool) {
		if !accept {
			return
		}
		a.run(func(ctx context.Context) error {
			if a.olm.Running(ctx) {
				if err := a.olm.Exit(ctx); err == nil {
					_ = a.olm.WaitStopped(ctx)
				}
			}
			return a.cli.Logout(ctx)
		}, "Logged out")
	})
}

func (a *App) selectAccount(acc pangolin.Account) {
	a.run(func(ctx context.Context) error {
		err := a.cli.SelectAccount(ctx, acc)
		if err != nil && pangolin.LoadAuth().Active.UserID == acc.UserID {
			err = nil
		}
		return err
	}, "Switched to "+acc.Label())
}

// runBusy is like run, but shows label as the app's busy state while f
// runs, which also locks the controls that would conflict with it.
func (a *App) runBusy(label string, f func(context.Context) error, ok string) {
	a.setBusy(label)
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, time.Minute)
		defer cancel()

		err := f(ctx)
		a.pollNow(ctx)
		glib.IdleAdd(func() {
			a.setBusy("")
			if err != nil {
				slog.Error("action failed", "err", err)
				a.toast(firstLine(err.Error()))
				return
			}
			if ok != "" {
				a.toast(ok)
			}
		})
	}()
}

func (a *App) selectOrg(id string) {
	if id == "" || id == tray.CurrentOrg(a.state) || a.state.Busy != "" {
		return
	}
	label := id
	if o, ok := a.state.Server.Org(id); ok {
		label = o.Label()
	}
	running := a.state.Tunnel != nil && a.state.Tunnel.Running
	a.runBusy("Switching organization…", func(ctx context.Context) error {
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := a.cli.SelectOrg(cctx, id)
		cancel()
		// With a running tunnel the CLI asks follow-up questions in a
		// terminal UI and fails without one, after saving the change.
		if err != nil && pangolin.LoadAuth().Active.OrgID == id {
			err = nil
		}
		if err != nil || !running {
			return err
		}
		return a.waitTunnel(ctx, connectTimeout, func(s *poller.TunnelStatus) (bool, error) {
			return !s.Running || (s.Online() && s.Status.OrgID == id), nil
		})
	}, "Switched to "+label)
}

// selectExitNode routes all traffic through the exit node with niceID, or
// stops using one when niceID is "". Without a running tunnel the choice
// is saved for the next connection.
func (a *App) selectExitNode(niceID string) {
	unchanged := niceID == "" && tray.CurrentExitNode(a.state) == 0 ||
		niceID != "" && niceID == CurrentExitNiceID(&a.state)
	if unchanged || a.state.Busy != "" {
		return
	}
	running := a.state.Tunnel != nil && a.state.Tunnel.Running

	if niceID == "" {
		ok := "No exit node will be used"
		if running {
			ok = "Stopped using the exit node"
		}
		gateway := running && a.state.Tunnel.Status != nil && a.state.Tunnel.Status.GatewayActive
		a.runBusy("Stopping exit node…", func(ctx context.Context) error {
			var errs []error
			if gateway {
				if err := a.olm.DisableGateway(ctx); err != nil {
					errs = append(errs, err)
				}
			}
			if err := pangolin.ClearSavedExitNode(); err != nil {
				errs = append(errs, fmt.Errorf("clear saved exit node: %w", err))
			}
			return errors.Join(errs...)
		}, ok)
		return
	}

	label := niceID
	resourceID := 0
	for _, e := range a.state.Server.ExitNodes {
		if e.NiceID == niceID {
			label, resourceID = e.Label(), e.ResourceID
		}
	}
	ok := label + " will be used when you connect"
	if running {
		ok = "Routing all traffic through " + label
	}
	a.runBusy("Switching exit node…", func(ctx context.Context) error {
		if err := a.cli.SelectExitNode(ctx, niceID); err != nil {
			return err
		}
		if !running || resourceID == 0 {
			return nil
		}
		// The tunnel applies the change asynchronously; give the status a
		// moment to catch up so the UI does not flicker back.
		_ = a.waitTunnel(ctx, 10*time.Second, func(s *poller.TunnelStatus) (bool, error) {
			return !s.Running || (s.Status != nil && s.Status.GatewayActive && s.Status.GatewayResource == resourceID), nil
		})
		return nil
	}, ok)
}

func (a *App) showSetup() {
	helper := ""
	body := "Traygolin will make the Pangolin CLI owned by the system so that its helper can start it. You will be asked for an administrator password once."
	if !privhelper.Installed() {
		if exe, err := os.Executable(); err == nil {
			helper = privhelper.HelperBesides(exe)
		}
		if _, err := os.Stat(helper); helper == "" || err != nil {
			Info{
				Heading: "Helper Not Found",
				Body:    "Traygolin's system helper was not found next to the app. Install Traygolin from your package manager or run <tt>sudo make install</tt>, which sets this up automatically.",
			}.Show(a, nil)
			return
		}
		body = "Traygolin will install a small system helper and a polkit rule so that you can connect without a password, like the official Pangolin apps. You will be asked for an administrator password once."
	}

	Confirmation{
		Heading: "Set Up Traygolin?",
		Body:    body,
		Accept:  "_Set Up",
		Reject:  "_Cancel",
	}.Show(a, func(accept bool) {
		if !accept {
			return
		}
		a.setBusy("Setting up…")
		go func() {
			ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
			defer cancel()
			err := privhelper.Setup(ctx, helper, traygolin.PolicyTemplate)
			if err == nil {
				_, err = privhelper.TrustedBinary(privhelper.TrustedPangolin, 0)
				if err != nil {
					err = fmt.Errorf("install the Pangolin CLI in /usr/local/bin or /usr/bin: %w", err)
				}
			}
			a.pollNow(ctx)
			glib.IdleAdd(func() {
				a.setBusy("")
				if err != nil {
					Info{Heading: "Setup Failed", Body: glib.MarkupEscapeText(err.Error())}.Show(a, nil)
					return
				}
				a.toast("Setup complete. You can connect now.")
			})
		}()
	})
}

func (a *App) resetDNS() {
	a.run(func(ctx context.Context) error { return a.cli.ResetDNS(ctx) }, "DNS settings restored")
}

func (a *App) checkUpdates() {
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 2*time.Minute)
		defer cancel()

		out, err := a.cli.Update(ctx)
		glib.IdleAdd(func() {
			if err != nil {
				a.toast(firstLine(err.Error()))
				return
			}
			msg := strings.TrimSpace(out)
			if msg == "" {
				msg = "The Pangolin CLI is up to date"
			}
			Info{Heading: "Pangolin CLI", Body: glib.MarkupEscapeText(msg)}.Show(a, nil)
		})
	}()
}

func (a *App) maybeConnectAtStart() {
	if a.autoConnect || a.settings == nil {
		return
	}
	st := &a.state
	if st.Tunnel == nil || st.Accounts == nil || st.CLI == nil {
		return
	}
	a.autoConnect = true
	if !a.settings.Boolean("connect-at-start") || st.Tunnel.Running {
		return
	}
	if cli, setup, login := OfflineNeeds(st); cli || setup || login {
		return
	}
	a.startTunnel()
}

func (a *App) getInterval() time.Duration {
	if a.pollOverride > 0 {
		return a.pollOverride
	}
	if a.settings == nil {
		return poller.DefaultInterval
	}
	return poller.ClampInterval(time.Duration(a.settings.Int("poll-interval")) * time.Second)
}

// Quit exits the app completely, causing Run to return.
func (a *App) Quit() {
	a.quitOnce.Do(func() {
		glib.IdleAdd(func() {
			a.tray.Close()
			a.app.Quit()
		})
	})
}

// Run runs the app, initializing everything and then entering the main
// loop. It returns when ctx is cancelled or Quit is called.
func (a *App) Run(ctx context.Context) int {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	a.ctx = ctx
	a.cli = &pangolin.Client{}
	a.olm = olm.New("")

	a.init(ctx)
	context.AfterFunc(ctx, a.Quit)

	if err := a.app.Register(ctx); err != nil {
		slog.Error("register application", "err", err)
		return 1
	}

	if !a.app.IsRemote() {
		a.poller = &poller.Poller{
			Interval: a.getInterval(),
			Olm:      a.olm,
			CLI:      a.cli,
			New:      func(s poller.Status) { glib.IdleAdd(func() { a.update(s) }) },
		}
		go a.poller.Run(ctx)
	}

	return a.app.Run(os.Args)
}
