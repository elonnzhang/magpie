package gui

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore
#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>

// The panel hangs from the menu bar, so its top edge stays where it is and
// the bottom moves; the system animates the frame on its own display clock.
static void glidePanel(void *w, int height, int ms, double x1, double y1, double x2, double y2) {
	NSWindow *win = (NSWindow *)w;
	dispatch_async(dispatch_get_main_queue(), ^{
		NSRect f = win.frame;
		NSRect to = NSMakeRect(f.origin.x, NSMaxY(f) - height, f.size.width, height);
		[NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
			ctx.duration = ms / 1000.0;
			ctx.timingFunction = [CAMediaTimingFunction functionWithControlPoints:x1 :y1 :x2 :y2];
			ctx.allowsImplicitAnimation = YES;
			[[win animator] setFrame:to display:YES];
		} completionHandler:nil];
	});
}
*/
import "C"

// glidePanel moves the shown panel to height, its top edge held under the
// menu bar. It says false when it can't, for the caller to size it plainly.
func (h *host) glidePanel(height int, g Glide) bool {
	w := h.panel.NativeWindow()
	if w == nil {
		return false
	}
	c := g.Curve
	C.glidePanel(w, C.int(height), C.int(g.MS), C.double(c[0]), C.double(c[1]), C.double(c[2]), C.double(c[3]))
	return true
}
