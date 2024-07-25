package editor

import (
	"context"
	"fmt"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4-sourceview/pkg/gtksource/v5"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/getseabird/seabird/api"
	"github.com/getseabird/seabird/internal/ctxt"
	"github.com/getseabird/seabird/internal/pubsub"
	"github.com/getseabird/seabird/internal/util"
	"github.com/getseabird/seabird/widget"
	"github.com/google/uuid"
	"github.com/hexops/gotextdiff"
	"github.com/hexops/gotextdiff/myers"
	"github.com/hexops/gotextdiff/span"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type EditorWindow struct {
	*adw.Window
	ctx     context.Context
	tabview *adw.TabView
	toast   *adw.ToastOverlay
	save    *gtk.Button
	pages   map[*adw.TabPage]*sourcePage
}

func NewEditorWindow(ctx context.Context) *EditorWindow {
	w := EditorWindow{
		Window: adw.NewWindow(),
		pages:  map[*adw.TabPage]*sourcePage{},
	}
	cluster := ctxt.MustFrom[*api.Cluster](ctx)
	w.SetDefaultSize(1000, 600)
	w.SetTitle(fmt.Sprintf("Editor - %v", cluster.ClusterPreferences.Value().Name))

	w.ConnectCloseRequest(func() (ok bool) {
		w.Hide()
		return true
	})

	ctx = ctxt.With[*gtk.Window](ctx, &w.Window.Window)
	w.ctx = ctx

	content := gtk.NewBox(gtk.OrientationVertical, 0)

	w.toast = adw.NewToastOverlay()
	w.toast.SetChild(content)
	w.SetContent(w.toast)

	toolbar := adw.NewToolbarView()
	toolbar.SetTopBarStyle(adw.ToolbarRaised)
	toolbar.AddCSSClass("view")
	content.Append(toolbar)

	header := adw.NewHeaderBar()
	toolbar.AddTopBar(header)

	w.save = gtk.NewButton()
	w.save.AddCSSClass("suggested-action")
	w.save.ConnectClicked(w.saveClicked)
	w.save.SetLabel("Apply")
	header.PackEnd(w.save)

	w.tabview = adw.NewTabView()
	toolbar.SetContent(w.tabview)

	w.tabview.ConnectClosePage(func(page *adw.TabPage) (ok bool) {
		for p := range w.pages {
			if p.Keyword() == page.Keyword() {
				delete(w.pages, p)
				break
			}
		}
		return !gdk.EVENT_STOP
	})

	tabbar := adw.NewTabBar()
	tabbar.SetView(w.tabview)
	toolbar.AddTopBar(tabbar)

	new := gtk.NewButton()
	new.SetIconName("text-editor-symbolic")
	new.ConnectClicked(func() {
		w.AddPage(nil, nil)
	})
	header.PackStart(new)

	return &w
}

func (w *EditorWindow) AddPage(gvk *schema.GroupVersionKind, object client.Object) error {
	if object != nil {
		for tp, p := range w.pages {
			if p.object == nil {
				continue
			}
			if p.object.GetUID() == object.GetUID() {
				w.tabview.SetSelectedPage(tp)
				return nil
			}
		}
	}
	title := pubsub.NewProperty("New Object")
	page, err := newSourcePage(w.ctx, gvk, object, title)
	if err != nil {
		return err
	}
	tabpage := w.tabview.Append(page)
	tabpage.SetKeyword(uuid.NewString())
	title.Sub(w.ctx, tabpage.SetTitle)
	w.tabview.SetSelectedPage(tabpage)
	w.pages[tabpage] = page
	return nil
}

func (w *EditorWindow) saveClicked() {
	cluster := ctxt.MustFrom[*api.Cluster](w.ctx)
	source := w.tabview.SelectedPage().Child().(*gtk.Paned).StartChild().(*gtk.ScrolledWindow).Child().(*gtksource.View)

	text := source.Buffer().Text(source.Buffer().StartIter(), source.Buffer().EndIter(), true)
	object, err := util.YamlToUnstructured([]byte(text))
	if err != nil {
		widget.ShowErrorDialog(w.ctx, "Error decoding object", err)
	}

	prevObj := object.DeepCopyObject().(client.Object)
	if err := cluster.Get(w.ctx, client.ObjectKeyFromObject(object), prevObj); err != nil {
		switch client.IgnoreNotFound(err) {
		case nil:
			if err := cluster.Create(w.ctx, object); err != nil {
				widget.ShowErrorDialog(w.ctx, "Error creating object", err)
				return
			}
			cluster.Get(w.ctx, client.ObjectKeyFromObject(object), object)
			if b, err := cluster.Encoder.EncodeYAML(object); err == nil {
				source.Buffer().SetText(string(b))
			}
			w.toast.AddToast(adw.NewToast("Object created."))
			return
		default:
			widget.ShowErrorDialog(w.ctx, "Error getting current object", err)
			return
		}
	}
	prev, err := cluster.Encoder.EncodeYAML(prevObj)
	if err != nil {
		widget.ShowErrorDialog(w.ctx, "Error encoding current object", err)
		return
	}

	dialog := adw.NewMessageDialog(ctxt.MustFrom[*gtk.Window](w.ctx), fmt.Sprintf("Saving %s", object.GetName()), "The following changes will be made")
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("save", "Save")
	dialog.SetResponseAppearance("save", adw.ResponseSuggested)
	dialog.SetSizeRequest(600, 500)
	defer dialog.Present()

	box := dialog.Child().(*gtk.WindowHandle).Child().(*gtk.Box).FirstChild().(*gtk.Box)

	box.FirstChild().(*gtk.Label).NextSibling().(*gtk.Label).SetVExpand(false)

	edits := myers.ComputeEdits(span.URIFromPath(object.GetName()), string(prev), text)

	dbuf := gtksource.NewBufferWithLanguage(gtksource.LanguageManagerGetDefault().Language("diff"))
	dbuf.SetText(strings.TrimPrefix(fmt.Sprint(gotextdiff.ToUnified("", "", string(prev), edits)), "--- \n+++ \n"))
	util.SetSourceColorScheme(dbuf)
	view := gtksource.NewViewWithBuffer(dbuf)
	view.SetEditable(false)
	view.SetWrapMode(gtk.WrapWord)
	view.SetShowLineNumbers(false)
	view.SetMonospace(true)

	sw := gtk.NewScrolledWindow()
	sw.SetChild(view)
	sw.SetVExpand(true)

	box.Append(sw)

	dialog.ConnectResponse(func(response string) {
		switch response {
		case "save":
			if err := cluster.Update(w.ctx, object); err != nil {
				widget.ShowErrorDialog(w.ctx, "Error updating object", err)
			}
			cluster.Get(w.ctx, client.ObjectKeyFromObject(object), object)
			if b, err := cluster.Encoder.EncodeYAML(object); err == nil {
				source.Buffer().SetText(string(b))
			}
			w.toast.AddToast(adw.NewToast("Object updated."))
		}
	})
}
