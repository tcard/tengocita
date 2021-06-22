import 'dart:html';
import 'dart:ui' as ui;
import 'package:flutter/widgets.dart';
import "dart:js" as js;

class QRCaptureController {
  void Function(String) callback;

  void onCapture(Function(String) f) {
    callback = f;
  }
}

class QRCaptureView extends StatelessWidget {
  static QRCaptureController controller;

  QRCaptureView({@required QRCaptureController controller}) {
    QRCaptureView.controller = controller;
  }

  static bool registeredPlatformView = false;
  static void registerPlatformView() {
    if (registeredPlatformView) {
      return;
    }
    registeredPlatformView = true;

    // ignore: undefined_prefixed_name
    ui.platformViewRegistry.registerViewFactory(
      'qr-scanner',
      (int viewId) {
        final e = CanvasElement()..hidden = true;
        js.context.callMethod('scanQR', [
          e,
          js.allowInterop((text) {
            if (controller.callback != null) {
              controller.callback(text);
            }
          })
        ]);
        return e;
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    registerPlatformView();
    return HtmlElementView(viewType: 'qr-scanner');
  }
}
