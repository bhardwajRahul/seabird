package ui

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/NdoleStudio/lemonsqueezy-go"
	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/getseabird/seabird/api"
	"github.com/getseabird/seabird/internal/ctxt"
	"github.com/getseabird/seabird/internal/style"
	"github.com/getseabird/seabird/internal/ui/common"
	"github.com/getseabird/seabird/widget"
	"github.com/imkira/go-observer/v2"
)

type WelcomeWindow struct {
	*widget.UniversalApplicationWindow
	*common.State
	ctx     context.Context
	content *adw.Bin
	nav     *adw.NavigationView
	toast   *adw.ToastOverlay
}

func NewWelcomeWindow(ctx context.Context, app *gtk.Application, state *common.State) *WelcomeWindow {
	window := widget.NewUniversalApplicationWindow(app)
	ctx = ctxt.With[*gtk.Window](ctx, &window.Window)
	w := WelcomeWindow{
		ctx:                        ctx,
		UniversalApplicationWindow: window,
		content:                    adw.NewBin(),
		State:                      state,
	}
	w.SetApplication(app)
	w.SetIconName("seabird")
	w.SetDefaultSize(600, 550)
	w.toast = adw.NewToastOverlay()
	w.toast.SetChild(w.content)
	w.SetContent(w.toast)
	w.content.SetChild(w.createContent())
	w.SetTitle(ApplicationName)

	var h glib.SignalHandle
	h = w.ConnectCloseRequest(func() bool {
		prefs := w.Preferences.Value()
		if err := prefs.Save(); err != nil {
			d := widget.ShowErrorDialog(ctx, "Could not save preferences", err)
			d.ConnectUnrealize(func() {
				w.Close()
			})
			w.HandlerDisconnect(h)
			return true
		}
		return false
	})

	return &w
}

func (w *WelcomeWindow) createContent() *adw.NavigationView {
	w.nav = adw.NewNavigationView()
	w.nav.ConnectPopped(func(page *adw.NavigationPage) {
		w.content.SetChild(w.createContent())
	})

	box := gtk.NewBox(gtk.OrientationVertical, 0)
	w.nav.Add(adw.NewNavigationPage(box, ApplicationName))

	if !style.Eq(style.Windows) {
		header := gtk.NewHeaderBar()
		box.Append(header)
	}

	page := adw.NewPreferencesPage()
	box.Append(page)

	if clusters := w.Preferences.Value().Clusters; len(clusters) > 0 {
		if w.Preferences.Value().License == nil {
			banner := adw.NewBanner("Your free trial expires in ∞ days")
			banner.SetRevealed(true)
			banner.SetButtonLabel("Purchase")
			banner.ConnectButtonClicked(func() {
				w.nav.Push(w.createPurchasePage())
			})
			banner.InsertBefore(box, page)
		}

		group := adw.NewPreferencesGroup()
		group.SetTitle("Connect to Cluster")
		page.Add(group)

		add := gtk.NewButton()
		add.AddCSSClass("flat")
		add.SetIconName("list-add")
		add.ConnectClicked(func() {
			pref := NewClusterPrefPage(w.ctx, w.State, observer.NewProperty(api.ClusterPreferences{}))
			w.nav.Push(pref.NavigationPage)
		})

		group.SetHeaderSuffix(add)

		for i, c := range w.Preferences.Value().Clusters {
			cluster := c
			row := adw.NewActionRow()
			row.SetTitle(cluster.Value().Name)
			row.SetActivatable(true)

			if kubeconfig := c.Value().Kubeconfig; kubeconfig != nil {
				label := gtk.NewLabel(kubeconfig.Path)
				label.AddCSSClass("dim-label")
				label.SetHAlign(gtk.AlignStart)
				row.AddSuffix(label)
			}

			spinner := widget.NewFallbackSpinner(gtk.NewImageFromIconName("go-next-symbolic"))
			row.AddSuffix(spinner)
			row.ConnectActivated(func() {
				if showClusterPrefsErrorDialog(w.ctx, cluster.Value()) {
					return
				}

				spinner.Start()
				go func() {
					state, err := w.NewClusterState(w.ctx, cluster)
					glib.IdleAdd(func() {
						spinner.Stop()
						if err != nil {
							widget.ShowErrorDialog(w.ctx, "Cluster connection failed", err)
							return
						}
						app := w.Application()
						w.Close()
						NewClusterWindow(w.ctx, app, state).Show()
					})
				}()
			})
			group.Add(row)
			if os.Getenv("SEABIRD_DEV") == "1" && i == 0 {
				defer row.Activate()
			}
		}
	} else {
		status := adw.NewStatusPage()
		status.SetIconName("seabird")
		status.SetTitle("No Clusters Found")
		status.SetDescription("Connect to a cluster to get started.")
		btn := gtk.NewButton()
		btn.ConnectClicked(func() {
			pref := NewClusterPrefPage(w.ctx, w.State, observer.NewProperty(api.ClusterPreferences{}))
			w.nav.Push(pref.NavigationPage)
		})
		btn.SetHAlign(gtk.AlignCenter)
		btn.SetLabel("New Cluster")
		btn.AddCSSClass("pill")
		btn.AddCSSClass("suggested-action")
		status.SetChild(btn)
		box.Append(status)
	}

	return w.nav
}

func (w *WelcomeWindow) createPurchasePage() *adw.NavigationPage {
	content := gtk.NewBox(gtk.OrientationVertical, 0)
	navPage := adw.NewNavigationPage(content, "Purchase Seabird")

	header := adw.NewHeaderBar()
	header.SetShowEndTitleButtons(!style.Eq(style.Windows))
	content.Append(header)

	prefPage := adw.NewPreferencesPage()
	content.Append(prefPage)

	group := adw.NewPreferencesGroup()
	group.SetDescription("There is no time limit for testing Seabird. With the purchase of a subscription, you receive extended support and help fund development.")
	prefPage.Add(group)

	action := adw.NewActionRow()
	action.SetTitle("Purchase now")
	action.SetActivatable(true)
	action.AddSuffix(gtk.NewImageFromIconName("go-next-symbolic"))
	action.ConnectActivated(func() {
		gtk.ShowURI(&w.Window, "https://seabird.lemonsqueezy.com/checkout/buy/7cbd80a0-701b-46cc-b61f-c46cc339dca5", gdk.CURRENT_TIME)
	})
	group.Add(action)

	entry := adw.NewEntryRow()
	entry.SetTitle("License key")
	entry.SetShowApplyButton(true)
	entry.ConnectApply(func() {
		res, raw, err := lemonsqueezy.New().Licenses.Activate(w.ctx, strings.TrimSpace(entry.Text()), "Seabird")
		switch {
		case err != nil:
			log.Printf("%v", err)
			err = errors.New(http.StatusText(raw.HTTPResponse.StatusCode))
			widget.ShowErrorDialog(w.ctx, "Could not activate license", err)
		case res.Activated:
			prefs := w.Preferences.Value()
			prefs.License = &api.License{
				ID:        res.Instance.ID,
				Key:       res.LicenseKey.Key,
				ExpiresAt: res.LicenseKey.ExpiresAt,
			}
			w.Preferences.Update(prefs)
			w.toast.AddToast(adw.NewToast("License activated. Thank you!"))
			w.nav.Pop()
		default:
			widget.ShowErrorDialog(w.ctx, "Could not activate license", errors.New(res.Error))
		}
	})
	group.Add(entry)

	return navPage
}
