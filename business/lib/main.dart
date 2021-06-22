import 'dart:convert';

import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:hive/hive.dart';
import 'package:hive_flutter/hive_flutter.dart';
import 'package:json_annotation/json_annotation.dart';
import 'package:http/http.dart' as http;
import 'package:flutter_spinkit/flutter_spinkit.dart';
import 'package:url_launcher/url_launcher.dart' as urlLauncher;
import 'package:calendar_strip/calendar_strip.dart';
import 'package:flutter_datetime_picker/flutter_datetime_picker.dart'
    show DatePicker;
import 'package:flutter_localizations/flutter_localizations.dart';
import "qr.dart"
    if (dart.library.io) "qrnative.dart"
    if (dart.library.js) "qrweb.dart" as qrcode;
part 'main.g.dart';

Future<void> main() async {
  await Hive.initFlutter();
  await Hive.openBox('root');
  runApp(BusinessApp());
}

class BusinessApp extends StatelessWidget {
  // This widget is the root of your application.
  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        primarySwatch: Colors.blue,
        visualDensity: VisualDensity.adaptivePlatformDensity,
      ),
      locale: Locale('es', 'ES'),
      localizationsDelegates: [
        GlobalMaterialLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
      ],
      supportedLocales: [
        const Locale('es'),
        const Locale('en'),
      ],
      home: Root(),
    );
  }
}

class Root extends StatefulWidget {
  @override
  _RootState createState() => _RootState();
}

class Auth {
  LoggedInBusiness business;
  _RootState rootState;

  void logOut() {
    rootState.setState(() {
      rootState.auth = null;
      Hive.box('root').delete('loggedInBusiness');
    });
  }

  Future<ServerResponse> action(String action, dynamic payload) async {
    (payload as Map<String, dynamic>)['authToken'] = business.authToken;
    final response = await serverAction(action, payload);
    if (response.result == 'invalidAuthToken') {
      logOut();
    }
    return response;
  }
}

class _RootState extends State<Root> {
  Auth auth;

  _RootState() {
    final businessJson = Hive.box('root').get('loggedInBusiness');
    final business = businessJson != null
        ? LoggedInBusiness.fromJson(json.decode(businessJson))
        : null;
    if (business != null) {
      auth = Auth()
        ..business = business
        ..rootState = this;
    }
  }

  @override
  Widget build(BuildContext context) => auth == null
      ? NotLoggedIn(onLoggedIn: (LoggedInBusiness business) {
          setState(() {
            auth = Auth()
              ..business = business
              ..rootState = this;
            Hive.box('root')
                .put('loggedInBusiness', json.encode(business.toJson()));
            Hive.box('root').put('hasEverLoggedIn', true);
          });
        })
      : LoggedIn(auth: auth);
}

@JsonSerializable()
class LoggedInBusiness {
  String authToken;
  String name;
  String email;
  String phone;
  String photo;
  String address;

  LoggedInBusiness();

  factory LoggedInBusiness.fromJson(Map<String, dynamic> json) =>
      _$LoggedInBusinessFromJson(json);

  Map<String, dynamic> toJson() => _$LoggedInBusinessToJson(this);
}

class NotLoggedIn extends StatefulWidget {
  final void Function(LoggedInBusiness) onLoggedIn;

  NotLoggedIn({this.onLoggedIn});

  @override
  _NotLoggedInState createState() => _NotLoggedInState(onLoggedIn: onLoggedIn);
}

enum _NotLoggedInSection {
  logIn,
  signUp,
}

class _NotLoggedInState extends State<NotLoggedIn> {
  final formKey = GlobalKey<FormState>();
  final void Function(LoggedInBusiness) onLoggedIn;
  _NotLoggedInSection section;

  String emailOrPhone;
  String password;
  final promoCode = TextEditingController(text: '');

  _NotLoggedInState({this.onLoggedIn}) {
    section = Hive.box('root').get('hasEverLoggedIn') == true
        ? _NotLoggedInSection.logIn
        : _NotLoggedInSection.signUp;
  }

  void submit(BuildContext context) async {
    final action = section == _NotLoggedInSection.signUp ? 'signup' : 'login';
    ServerResponse response;
    try {
      response = await serverAction(action, {
        'emailOrPhone': emailOrPhone,
        'password': password,
        'promoCode': promoCode.text,
      });
    } catch (e, s) {
      unexpectedErrorFeedback(context, e, s);
      return;
    }

    switch (response.result) {
      case 'missingEmailOrPhone':
        Scaffold.of(context).showSnackBar(
            SnackBar(content: Text('Introduce un email o teléfono válido')));
        break;
      case 'missingPassword':
        Scaffold.of(context).showSnackBar(
            SnackBar(content: Text('Introduce una contraseña válida')));
        break;
      case 'emailOrPhoneTaken':
        Scaffold.of(context).showSnackBar(
            SnackBar(content: Text('El email o teléfono ya existe')));
        break;
      case 'badCredentials':
        Scaffold.of(context).showSnackBar(SnackBar(
            content: Text('Email, teléfono o contraseña incorrectos')));
        break;
      case 'badPromoCode':
        Scaffold.of(context).showSnackBar(
            SnackBar(content: Text('Código de promoción no válido')));
        break;
      case 'signedUp':
      case 'loggedIn':
        final business = LoggedInBusiness.fromJson(response.payload);
        onLoggedIn(business);
        break;
      default:
        // TODO: log? report?
        return;
    }
  }

  bool acceptedTOS = false;

  @override
  Widget build(BuildContext context) => Scaffold(
        body: SafeArea(
          child: Builder(
            builder: (BuildContext context) => Padding(
              padding: EdgeInsets.all(8.0),
              child: Form(
                key: formKey,
                child: SingleChildScrollView(
                    child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                        Container(
                          padding: EdgeInsets.symmetric(vertical: 16.0),
                          alignment: Alignment.center,
                          child: Column(children: <Widget>[
                            Image(
                              image: AssetImage('assets/icon.png'),
                              width: 150.0,
                              height: 150.0,
                            ),
                            Padding(
                                padding: EdgeInsets.symmetric(vertical: 16.0),
                                child: Text(
                                  'TengoCita',
                                  style: TextStyle(
                                    color: Color.fromARGB(255, 38, 59, 56),
                                    fontWeight: FontWeight.bold,
                                    fontSize: 24,
                                  ),
                                )),
                          ]),
                        ),
                        Container(
                          padding: EdgeInsets.symmetric(vertical: 16.0),
                          alignment: Alignment.center,
                          child: Text(
                            section == _NotLoggedInSection.logIn
                                ? '¿Ya tienes una cuenta?'
                                : '¿Aún no tienes una cuenta?',
                            style: Theme.of(context).textTheme.headline6,
                          ),
                        ),
                        TextFormField(
                          decoration: const InputDecoration(
                            icon: Icon(Icons.person),
                            labelText: 'Dirección de email o teléfono',
                          ),
                          validator: (text) => text.isEmpty
                              ? 'Introduce tu dirección de email o teléfono'
                              : null,
                          onChanged: (text) {
                            emailOrPhone = text;
                          },
                        ),
                        TextFormField(
                          obscureText: true,
                          decoration: const InputDecoration(
                            icon: Icon(Icons.vpn_key),
                            labelText: 'Contraseña',
                          ),
                          validator: (text) =>
                              text.isEmpty ? 'Introduce tu contraseña' : null,
                          onChanged: (text) {
                            password = text;
                          },
                        ),
                      ] +
                      (section == _NotLoggedInSection.signUp
                          ? <Widget>[
                              TextFormField(
                                obscureText: true,
                                decoration: const InputDecoration(
                                  icon: Icon(Icons.card_giftcard),
                                  labelText: '¿Tienes un código promocional?',
                                ),
                                controller: promoCode,
                              )
                            ]
                          : <Widget>[]) +
                      <Widget>[
                        Container(
                          padding: EdgeInsets.symmetric(vertical: 16.0),
                          alignment: Alignment.center,
                          child: Column(
                              children: (section == _NotLoggedInSection.signUp
                                      ? <Widget>[
                                          Row(
                                              mainAxisAlignment:
                                                  MainAxisAlignment.center,
                                              children: <Widget>[
                                                Checkbox(
                                                  onChanged: (value) {
                                                    setState(() {
                                                      acceptedTOS = value;
                                                    });
                                                  },
                                                  value: acceptedTOS,
                                                ),
                                                Flexible(
                                                    child: RichText(
                                                  text: TextSpan(
                                                      style: Theme.of(context)
                                                          .textTheme
                                                          .bodyText1,
                                                      text:
                                                          'He leído y acepto las ',
                                                      children: <TextSpan>[
                                                        TextSpan(
                                                          text:
                                                              'condiciones de uso y política de privacidad',
                                                          style: TextStyle(
                                                            decoration:
                                                                TextDecoration
                                                                    .underline,
                                                          ),
                                                          recognizer:
                                                              TapGestureRecognizer()
                                                                ..onTap = () =>
                                                                    urlLauncher
                                                                        .launch(
                                                                            'https://tengocita.app/condiciones.html'),
                                                        ),
                                                        TextSpan(text: '.'),
                                                      ]),
                                                )),
                                              ]),
                                        ]
                                      : <Widget>[]) +
                                  <Widget>[
                                    RaisedButton(
                                      child: Text(
                                          section == _NotLoggedInSection.logIn
                                              ? 'ENTRAR EN TU CUENTA'
                                              : 'CREAR UNA CUENTA'),
                                      onPressed: (section ==
                                                  _NotLoggedInSection.logIn ||
                                              acceptedTOS)
                                          ? () {
                                              if (!formKey.currentState
                                                  .validate()) {
                                                return;
                                              }
                                              submit(context);
                                            }
                                          : null,
                                    ),
                                    FlatButton(
                                        child: Text(
                                            section == _NotLoggedInSection.logIn
                                                ? 'AÚN NO TENGO UNA CUENTA'
                                                : 'YA TENGO UNA CUENTA'),
                                        onPressed: () {
                                          setState(() {
                                            section = section ==
                                                    _NotLoggedInSection.logIn
                                                ? _NotLoggedInSection.signUp
                                                : _NotLoggedInSection.logIn;
                                          });
                                        })
                                  ]),
                        )
                      ],
                )),
              ),
            ),
          ),
        ),
      );
}

class LoggedIn extends StatefulWidget {
  final Auth auth;

  LoggedIn({this.auth});

  @override
  State<StatefulWidget> createState() => _LoggedInState(auth: auth);
}

class LoggedInSection {
  final BottomNavigationBarItem barItem;
  final Widget Function({Auth auth, void Function(int) changeTab}) widget;

  LoggedInSection(this.barItem, this.widget);

  static final sections = [
    LoggedInSection(
      BottomNavigationBarItem(
        icon: Icon(Icons.today),
        title: Text('Agenda'),
      ),
      ({Auth auth, void Function(int) changeTab}) => Agenda(auth: auth),
    ),
    LoggedInSection(
      BottomNavigationBarItem(
        icon: Icon(Icons.camera_alt),
        title: Text('Llegada'),
      ),
      ({Auth auth, void Function(int) changeTab}) =>
          Arrival(auth: auth, changeTab: changeTab),
    ),
    LoggedInSection(
      BottomNavigationBarItem(
        icon: Icon(Icons.settings),
        title: Text('Configuración'),
      ),
      ({Auth auth, void Function(int) changeTab}) => Config(auth: auth),
    ),
  ];
}

class _LoggedInState extends State<LoggedIn> {
  final Auth auth;
  int section = 0;

  _LoggedInState({this.auth});

  @override
  Widget build(BuildContext context) {
    var sections = LoggedInSection.sections;
    if (auth.business.name == null) {
      sections = [sections[2]];
      section = 0;
    }
    return Scaffold(
      body: SafeArea(
          child: sections[section].widget(
              auth: auth,
              changeTab: (tab) {
                setState(() {
                  section = tab;
                });
              })),
      bottomNavigationBar: sections.length > 1
          ? BottomNavigationBar(
              currentIndex: section,
              onTap: (section) {
                setState(() {
                  this.section = section;
                });
              },
              items: sections.map((s) => s.barItem).toList(),
            )
          : null,
    );
  }
}

class Agenda extends StatefulWidget {
  final Auth auth;

  Agenda({this.auth});

  @override
  State<StatefulWidget> createState() => _AgendaState(auth: auth);
}

@JsonSerializable()
class Appointment {
  String id;
  int number;
  DateTime start;
  DateTime end;
  String phone;
  String email;
  DateTime startedAt;
  String name;

  Appointment();

  factory Appointment.fromJson(Map<String, dynamic> json) {
    final a = _$AppointmentFromJson(json);
    a.start = a.start != null ? a.start.toLocal() : null;
    a.end = a.end != null ? a.end.toLocal() : null;
    a.startedAt = a.startedAt != null ? a.startedAt.toLocal() : null;
    return a;
  }
}

class AgendaChunk {
  DateTime start;
  final appointments = <AppointmentInChunk>[];
}

class AppointmentInChunk {
  Appointment appointment;
  AppointmentStatus status;
}

enum AppointmentStatus {
  starts,
  continues,
}

List<AgendaChunk> agendaChunksFromAppointments(List<Appointment> appointments) {
  final chunksByStart = <DateTime, AgendaChunk>{};

  for (final app in appointments) {
    chunksByStart[app.start] = AgendaChunk()..start = app.start;
    chunksByStart[app.end] = AgendaChunk()..start = app.end;
  }

  final chunks = chunksByStart.values.toList();
  chunks.sort((a, b) => a.start.compareTo(b.start));

  out:
  for (final app in appointments) {
    bool started = false;
    for (final chunk in chunks) {
      if (chunk.start == app.start) {
        chunk.appointments.add(AppointmentInChunk()
          ..appointment = app
          ..status = AppointmentStatus.starts);
        started = true;
        continue;
      }
      if (!chunk.start.isBefore(app.end)) {
        continue out;
      }
      if (started) {
        chunk.appointments.add(AppointmentInChunk()
          ..appointment = app
          ..status = AppointmentStatus.continues);
      }
    }
  }

  return chunks;
}

class _AgendaState extends State<Agenda> {
  final Auth auth;
  final DateTime today = DateTime.now().atBeginning();
  DateTime day;

  _AgendaState({this.auth}) {
    day = today;
  }

  List<AgendaChunk> agenda;

  final changing = new Set<String>();

  Future<void> pickDay(DateTime day) async {
    if (day == null) {
      return;
    }
    day = day.atBeginning();
    setState(() {
      this.day = day;
      this.agenda = null;
    });
    try {
      final response = await auth.action('listActiveAppointments', {
        'start': day.formatJson(),
        'end': day.add(Duration(days: 1)).formatJson(),
      });
      switch (response.result) {
        case 'appointments':
          setState(() {
            agenda = agendaChunksFromAppointments(
                (response.payload as List<dynamic>)
                    .map((a) => Appointment.fromJson(a as Map<String, dynamic>))
                    .toList());
          });
      }
    } on HttpResponseException catch (e, s) {
      unexpectedErrorFeedback(context, e, s);
    }
  }

  @override
  void initState() {
    super.initState();
    pickDay(day);
  }

  @override
  Widget build(BuildContext context) => Column(
      children: <Widget>[
            CalendarStrip(
              // selectedDate: day,
              onDateSelected: (date) {
                pickDay(date);
              },
            )
          ] +
          (day.isBefore(today)
              ? []
              : <Widget>[
                  Center(
                    child: RaisedButton.icon(
                      icon: Icon(Icons.add_alarm),
                      label: Text('NUEVA CITA'),
                      onPressed: () async {
                        await Navigator.push(
                            context,
                            MaterialPageRoute(
                                builder: (context) =>
                                    NewAppointment(day: day, auth: auth)));
                        pickDay(day);
                      },
                    ),
                  )
                ]) +
          <Widget>[
            Expanded(
              child: agenda == null
                  ? SpinKitRing(
                      color: Colors.grey,
                      size: 50.0,
                    )
                  : ListView.builder(
                      itemCount: agenda.length + 1,
                      itemBuilder: (BuildContext context, int index) => index ==
                              agenda.length
                          ? Center(
                              child: Text(
                                'No hay más citas este día.',
                                style: TextStyle(color: Colors.grey),
                              ),
                            )
                          : Column(children: [
                              Stack(
                                alignment: Alignment.center,
                                children: [
                                  Divider(
                                    thickness: 1.0,
                                    color: Colors.grey,
                                  ),
                                  Container(
                                    alignment: Alignment.center,
                                    padding:
                                        EdgeInsets.symmetric(vertical: 8.0),
                                    child: Text(
                                        ' ${agenda[index].start.hour.toString().padLeft(2, '0')}:${agenda[index].start.minute.toString().padLeft(2, '0')} ',
                                        style: TextStyle(
                                            fontWeight: FontWeight.bold,
                                            backgroundColor: Theme.of(context)
                                                .scaffoldBackgroundColor)),
                                  ),
                                ],
                              ),
                              Container(
                                alignment: Alignment.topLeft,
                                padding: EdgeInsets.symmetric(horizontal: 8.0),
                                child: Column(
                                  children: agenda[index]
                                      .appointments
                                      .map((a) => buildAppointment(context, a))
                                      .toList(),
                                ),
                              ),
                            ]),
                    ),
            ),
          ]);

  Widget buildAppointment(BuildContext context, AppointmentInChunk a) => Stack(
        children: <Widget>[
              Row(
                  children: <Widget>[
                        Text(
                            '#${a.appointment.number}${a.status == AppointmentStatus.continues ? ' continúa' : ''}'),
                      ] +
                      (a.status == AppointmentStatus.continues
                          ? <Widget>[]
                          : (a.appointment.name == null
                                  ? <Widget>[]
                                  : <Widget>[
                                      FlatButton.icon(
                                        onPressed: () {
                                          showDialog(
                                              context: context,
                                              builder: (context) => Dialog(
                                                    child: Text(
                                                        a.appointment.name),
                                                  ));
                                        },
                                        icon: Icon(Icons.person),
                                        label: Text(nameAndInitials(
                                            a.appointment.name)),
                                      ),
                                    ]) +
                              <Widget>[
                                FlatButton.icon(
                                  onPressed: () async {
                                    final url = a.appointment.phone != null
                                        ? whatsappURL(a.appointment.phone)
                                        : 'mailto:${a.appointment.email}';
                                    if (await urlLauncher.canLaunch(url)) {
                                      urlLauncher.launch(url);
                                    }
                                  },
                                  icon: Icon(a.appointment.phone != null
                                      ? Icons.chat
                                      : Icons.email),
                                  label: Text(a.appointment.phone != null
                                      ? a.appointment.phone
                                      : a.appointment.email),
                                ),
                              ]))
            ] +
            (a.status == AppointmentStatus.continues
                ? <Widget>[]
                : <Widget>[
                    Align(
                      alignment: Alignment.topRight,
                      child: a.appointment.startedAt == null
                          ? FlatButton(
                              textColor: Colors.red,
                              onPressed: changing.contains(a.appointment.id)
                                  ? null
                                  : () async {
                                      final reason = await Navigator.push(
                                          context,
                                          MaterialPageRoute(
                                            builder: (context) =>
                                                CancelAppointmentReason(),
                                          ));
                                      if (reason == null) {
                                        return;
                                      }
                                      setState(() {
                                        changing.add(a.appointment.id);
                                      });
                                      try {
                                        await cancelAppointment(a.appointment,
                                            reason: reason);
                                      } finally {
                                        setState(() {
                                          changing.remove(a.appointment.id);
                                        });
                                      }
                                    },
                              child: Text('ANULAR'),
                            )
                          : RaisedButton.icon(
                              textColor: Colors.white,
                              color: Colors.green,
                              onPressed: changing.contains(a.appointment.id)
                                  ? null
                                  : () async {
                                      setState(() {
                                        changing.add(a.appointment.id);
                                      });
                                      try {
                                        await finishAppointment(
                                            a.appointment.id);
                                      } finally {
                                        setState(() {
                                          changing.remove(a.appointment.id);
                                        });
                                      }
                                    },
                              icon: Icon(Icons.done),
                              label: Text('TERMINAR'),
                            ),
                    ),
                  ]),
      );

  Future<void> cancelAppointment(Appointment app, {String reason}) async {
    try {
      final response = await auth.action('cancelAppointment', {
        'id': app.id,
        'reason': reason,
      });
      switch (response.result) {
        case 'canceled':
          if (app.phone != null &&
              response.payload['customerMessage'] != null) {
            urlLauncher.launch(
                whatsappURL(app.phone, response.payload['customerMessage']));
          }
          pickDay(day);
          break;
      }
    } catch (e, s) {
      unexpectedErrorFeedback(context, e, s);
    }
  }

  Future<void> finishAppointment(String id) async {
    try {
      final response = await auth.action('finishAppointment', {'id': id});
      switch (response.result) {
        case 'ok':
          pickDay(day);
          break;
      }
    } catch (e, s) {
      unexpectedErrorFeedback(context, e, s);
    }
  }
}

class CancelAppointmentReason extends StatelessWidget {
  final reason = TextEditingController();

  @override
  Widget build(BuildContext context) => Scaffold(
        appBar: AppBar(title: Text('Anulando cita')),
        body: Padding(
          padding: EdgeInsets.all(8.0),
          child: Column(children: [
            TextField(
              decoration: const InputDecoration(
                icon: Icon(Icons.comment),
                labelText: 'Motivo (opcional) (se envía al cliente)',
              ),
              keyboardType: TextInputType.multiline,
              controller: reason,
            ),
            ButtonBar(
              alignment: MainAxisAlignment.end,
              children: [
                FlatButton(
                  child: Text('CANCELAR'),
                  onPressed: () {
                    Navigator.pop(context);
                  },
                ),
                RaisedButton(
                  child: Text('ANULAR CITA'),
                  color: Colors.red,
                  onPressed: () {
                    Navigator.pop(context, reason.text);
                  },
                ),
              ],
            ),
          ]),
        ),
      );
}

class Arrival extends StatefulWidget {
  final Auth auth;
  final void Function(int) changeTab;

  Arrival({this.auth, this.changeTab});

  @override
  State<StatefulWidget> createState() =>
      _ArrivalState(auth: auth, changeTab: changeTab);
}

class _ArrivalState extends State<Arrival> {
  final Auth auth;
  final void Function(int) changeTab;

  _ArrivalState({this.auth, this.changeTab});

  final formKey = GlobalKey<FormState>();
  final qrController = qrcode.QRCaptureController();
  final code = TextEditingController();

  String qrCaptured;
  bool scanning = false;

  @override
  void initState() {
    super.initState();
    qrController.onCapture((captured) {
      if (captured != null && !scanning && qrCaptured == null) {
        setState(() {
          qrCaptured = captured;
        });
      }
    });
  }

  void scanAppointment(BuildContext context, {int code, String id}) async {
    if (scanning) {
      return;
    }
    setState(() {
      scanning = true;
      qrCaptured = null;
    });
    try {
      final response = await auth.action('startAppointment', {
        'code': code,
        'id': id,
      });
      switch (response.result) {
        case 'started':
          if (response.payload['meanDelay'] != null) {
            final meanDelay = Duration(
                microseconds: (response.payload['meanDelay'] / 1000).toInt());
            if (meanDelay > Duration(minutes: 7)) {
              final sendNotice = await showDialog(
                context: context,
                builder: (context) => AlertDialog(
                  title: Text('Aviso de retraso'),
                  content: Text(
                      'Retraso medio de ${meanDelay.inMinutes} minutos en las últimas citas. ¿Avisar a las próximas citas?'),
                  actions: [
                    FlatButton(
                      child: Text('NO AVISAR'),
                      onPressed: () {
                        Navigator.pop(context, false);
                      },
                    ),
                    FlatButton(
                      child: Text('AVISAR'),
                      color: Colors.red,
                      onPressed: () {
                        Navigator.pop(context, true);
                      },
                    ),
                  ],
                ),
              );

              if (sendNotice) {
                await auth.action('delayAlert', <String, dynamic>{});
              }
            }
          }

          Scaffold.of(context).showSnackBar(SnackBar(
              content: Text(
                  'Cita #${response.payload['appointment']['number']} recibida.')));
          changeTab(0);
          break;
        case 'notFound':
          Scaffold.of(context)
              .showSnackBar(SnackBar(content: Text('Cita no encontrada.')));
          break;
      }
    } catch (e, s) {
      unexpectedErrorFeedback(context, e, s);
    } finally {
      setState(() {
        scanning = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    if (qrCaptured != null) {
      scanAppointment(context, id: qrCaptured);
    }
    return Center(
        child: Column(
            children: !scanning
                ? <Widget>[
                    SizedBox(height: 16.0),
                    Text('Escanea el código QR de la cita:'),
                    Container(
                      width: 320.0,
                      height: 240.0,
                      padding: EdgeInsets.all(16.0),
                      child: qrcode.QRCaptureView(controller: qrController),
                    ),
                    Text('o introduce el código numérico:'),
                    Form(
                      key: formKey,
                      child: SingleChildScrollView(
                        child: Column(children: [
                          SizedBox(
                            width: 170,
                            child: TextFormField(
                              controller: code,
                              keyboardType: TextInputType.number,
                              style: TextStyle(
                                fontWeight: FontWeight.bold,
                                fontSize: 48.0,
                              ),
                              maxLength: 6,
                              maxLengthEnforced: true,
                              validator: (text) {
                                if (!appCodeRegexp.hasMatch(text)) {
                                  return 'Introduce un código de 6 cifras';
                                }
                                final digits = text
                                    .split('')
                                    .map((c) => int.parse(c))
                                    .toList();
                                if (digits[0] +
                                        digits[1] * 2 +
                                        digits[2] * 3 +
                                        digits[3] * 4 !=
                                    digits[4] * 10 + digits[5]) {
                                  return 'Código incorrecto';
                                }
                                return null;
                              },
                            ),
                          ),
                          RaisedButton(
                            child: Text('PROCESAR CÓDIGO'),
                            onPressed: () {
                              if (!formKey.currentState.validate()) {
                                return;
                              }
                              scanAppointment(context,
                                  code: int.parse(code.text));
                            },
                          ),
                        ]),
                      ),
                    ),
                  ]
                : [
                    Expanded(
                      child: Align(
                        alignment: Alignment.center,
                        child: SpinKitRing(
                          color: Colors.grey,
                          size: 50.0,
                        ),
                      ),
                    ),
                  ]));
  }
}

class Config extends StatelessWidget {
  final Auth auth;

  final formKey = GlobalKey<FormState>();
  final name = TextEditingController();
  final phone = TextEditingController();
  final email = TextEditingController();
  final address = TextEditingController();

  Config({this.auth}) {
    name.text = auth.business.name;
    phone.text = auth.business.phone;
    email.text = auth.business.email;
    address.text = auth.business.address;
  }

  void submit(BuildContext context) async {
    Scaffold.of(context).showSnackBar(SnackBar(content: Text('Guardando...')));
    ServerResponse response;
    try {
      response = await auth.action('configureBusiness', {
        'name': name.text,
        'email': email.text,
        'phone': phone.text,
        'address': address.text,
      });
    } catch (e, s) {
      unexpectedErrorFeedback(context, e, s);
      return;
    }

    Scaffold.of(context).removeCurrentSnackBar();
    switch (response.result) {
      case 'business':
        auth.rootState.setState(() {
          final authToken = auth.business.authToken;
          auth.business = LoggedInBusiness.fromJson(response.payload);
          auth.business.authToken = authToken;
        });
        Hive.box('root')
            .put('loggedInBusiness', json.encode(auth.business.toJson()));
        Scaffold.of(context)
            .showSnackBar(SnackBar(content: Text('Datos guardados.')));
        break;
      case 'emailTaken':
        Scaffold.of(context).showSnackBar(
            SnackBar(content: Text('La dirección de email ya está en uso')));
        break;
      case 'phoneTaken':
        Scaffold.of(context).showSnackBar(
            SnackBar(content: Text('El teléfono ya está en uso')));
        break;
    }
  }

  @override
  Widget build(BuildContext context) => Scaffold(
        appBar:
            auth.business.name == null ? AppBar(title: Text('¡Hola!')) : null,
        body: Padding(
          padding: EdgeInsets.all(8.0),
          child: Form(
            key: formKey,
            child: Column(
              children: [
                TextFormField(
                  controller: name,
                  decoration: const InputDecoration(
                    icon: Icon(Icons.people),
                    labelText: 'Nombre comercial',
                  ),
                  validator: (text) => text.isEmpty
                      ? 'Introduce un nombre de cara al público'
                      : null,
                ),
                TextFormField(
                  controller: phone,
                  decoration: const InputDecoration(
                    icon: Icon(Icons.phone),
                    labelText: 'Teléfono (opcional si hay email)',
                  ),
                  keyboardType: TextInputType.phone,
                  validator: (text) {
                    if (text.isEmpty && email.text.isEmpty) {
                      return 'Debes introducir al menos un email o teléfono';
                    }
                    return null;
                  },
                ),
                TextFormField(
                  controller: email,
                  decoration: const InputDecoration(
                    icon: Icon(Icons.email),
                    labelText: 'Dirección de email (opcional si hay teléfono)',
                  ),
                  keyboardType: TextInputType.emailAddress,
                  validator: (text) {
                    if (text.isEmpty && phone.text.isEmpty) {
                      return 'Debes introducir al menos un email o teléfono';
                    }
                    if (text.isNotEmpty && !text.contains('@')) {
                      return 'Debe tener una @';
                    }
                    return null;
                  },
                ),
                TextFormField(
                  controller: address,
                  decoration: const InputDecoration(
                    icon: Icon(Icons.map),
                    labelText: 'Dirección postal completa (opcional)',
                  ),
                  keyboardType: TextInputType.multiline,
                  maxLines: 5,
                ),
                ButtonBar(
                  alignment: MainAxisAlignment.end,
                  children: [
                    RaisedButton(
                      child: Text('GUARDAR'),
                      onPressed: () {
                        if (!formKey.currentState.validate()) {
                          return;
                        }
                        submit(context);
                      },
                    ),
                  ],
                ),
                Expanded(
                    child: Align(
                  alignment: Alignment.bottomCenter,
                  child: Row(children: [
                    Expanded(
                      child: RaisedButton(
                        color: Colors.red,
                        textColor: Colors.white,
                        child: Text('CERRAR SESIÓN'),
                        onPressed: () {
                          auth.logOut();
                        },
                      ),
                    ),
                  ]),
                )),
              ],
            ),
          ),
        ),
      );
}

Future<ServerResponse> serverAction(String action, dynamic payload) async {
  final path = 'https://tengocita.app/$action';
  final response = await http.post(
    path,
    body: json.encode(payload),
  );
  if (response.statusCode != 200) {
    throw HttpResponseException(
      statusCode: response.statusCode,
      path: path,
    );
  }
  return _$ServerResponseFromJson(json.decode(response.body));
}

class HttpResponseException implements Exception {
  final int statusCode;
  final String path;

  HttpResponseException({this.statusCode, this.path});

  String toString() => 'Status code $statusCode from $path';
}

@JsonSerializable()
class ServerResponse {
  String result;
  dynamic payload;
}

class NewAppointment extends StatefulWidget {
  final Auth auth;
  final DateTime day;

  NewAppointment({@required this.auth, @required this.day});

  @override
  State<StatefulWidget> createState() =>
      _NewAppointmentState(auth: auth, day: day);
}

class _NewAppointmentState extends State<NewAppointment> {
  final Auth auth;

  TextEditingController day;
  // final email = TextEditingController();
  final phone = TextEditingController();
  final startTime = TextEditingController();
  final endTime = TextEditingController();
  final name = TextEditingController();
  final comments = TextEditingController();

  _NewAppointmentState({@required this.auth, @required DateTime day}) {
    this.day = TextEditingController(text: day.formatDMY());
  }

  final formKey = GlobalKey<FormState>();

  void submit(BuildContext context) async {
    try {
      final dayDate = tryParseDMY(day.text);
      final startDate =
          dayDate.add(tryParseHM(startTime.text).sinceBeginningOfDay());
      final endTimeOfDay = tryParseHM(endTime.text);
      final endDate = endTimeOfDay == null
          ? null
          : dayDate.add(endTimeOfDay.sinceBeginningOfDay());

      Scaffold.of(context)
          .showSnackBar(SnackBar(content: Text('Guardando...')));

      final response = await auth.action('newAppointment', {
        'start': startDate.formatJson(),
        'end': endDate == null ? null : endDate.formatJson(),
        // 'email': email.text,
        'phone': phone.text,
        'name': name.text,
        'comments': comments.text,
      });
      Scaffold.of(context).removeCurrentSnackBar();
      switch (response.result) {
        case 'created':
          if (phone.text.isNotEmpty) {
            await urlLauncher.launch(
                whatsappURL(phone.text, response.payload['customerMessage']));
          }

          Navigator.pop(context, true);
          Scaffold.of(context)
              .showSnackBar(SnackBar(content: Text('Cita creada.')));
          break;
      }
    } catch (e, s) {
      unexpectedErrorFeedback(context, e, s);
    }
  }

  Future<TimeOfDay> pickTime(TextEditingController controller) async {
    var time = tryParseHM(controller.text);
    if (time == null) {
      time = TimeOfDay.now();
    }
    final picked = await DatePicker.showTimePicker(
      context,
      showSecondsColumn: false,
      currentTime: DateTime(1).add(time.sinceBeginningOfDay()),
    );
    if (picked != null) {
      time = TimeOfDay.fromDateTime(picked);
      setState(() {
        controller.text = time.formatHM();
      });
      return time;
    }
    return null;
  }

  @override
  Widget build(BuildContext context) => Scaffold(
      appBar: AppBar(title: Text('Nueva cita')),
      body: Builder(
        builder: (BuildContext context) => Padding(
          padding: EdgeInsets.all(8.0),
          child: Form(
            key: formKey,
            child: SingleChildScrollView(
                child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                TextFormField(
                  controller: day,
                  decoration: const InputDecoration(
                    icon: Icon(Icons.calendar_today),
                    labelText: 'Día',
                  ),
                  validator: (text) {
                    final date = tryParseDMY(text);
                    if (date == null) {
                      return 'Introduce o selecciona una fecha en formato dd/mm/yyyy';
                    }
                    if (date.isBefore(DateTime.now().atBeginning())) {
                      return 'La fecha no puede ser anterior al día de hoy';
                    }
                    return null;
                  },
                  onTap: () async {
                    final dayDate = tryParseDMY(day.text);
                    day.text = ((await showDatePicker(
                              context: context,
                              initialDate: dayDate,
                              firstDate: dayDate,
                              lastDate: dayDate.add(Duration(days: 1000)),
                              fieldLabelText: 'Día',
                            )) ??
                            dayDate)
                        .formatDMY();
                  },
                ),
                Row(children: <Widget>[
                  Flexible(
                    child: TextFormField(
                      decoration: const InputDecoration(
                        labelText: 'Hora de comienzo',
                        icon: Icon(Icons.access_time),
                      ),
                      controller: startTime,
                      validator: (text) {
                        final start = tryParseHM(text);
                        if (start == null) {
                          return 'Introduce o selecciona una hora en formato hh:mm';
                        }
                        final end = tryParseHM(endTime.text);
                        if (end != null &&
                            (end.hour < start.hour ||
                                (end.hour == start.hour &&
                                    end.minute < start.minute))) {
                          return 'Fin anterior a comienzo';
                        }
                        return null;
                      },
                      onTap: () async {
                        final picked = await pickTime(startTime);
                        if (endTime.text.isEmpty) {
                          endTime.text =
                              picked.add(Duration(minutes: 5)).formatHM();
                        }
                      },
                    ),
                  ),
                  Flexible(
                    child: TextFormField(
                      decoration: const InputDecoration(
                        labelText: 'Hora de fin',
                        icon: Icon(Icons.alarm_off),
                      ),
                      controller: endTime,
                      validator: (text) {
                        if (text.isEmpty) {
                          return null;
                        }
                        final end = tryParseHM(text);
                        if (end == null) {
                          return 'Introduce o selecciona una hora en formato hh:mm';
                        }
                        final start = tryParseHM(startTime.text);
                        if (end != null &&
                            (end.hour < start.hour ||
                                (end.hour == start.hour &&
                                    end.minute < start.minute))) {
                          return 'Fin anterior a comienzo';
                        }
                        return null;
                      },
                      onTap: () async {
                        pickTime(endTime);
                      },
                    ),
                  ),
                ]),
                TextFormField(
                  decoration: const InputDecoration(
                    icon: Icon(Icons.phone),
                    // labelText: 'Teléfono móvil (opcional si hay email)',
                    labelText: 'Teléfono móvil',
                  ),
                  keyboardType: TextInputType.phone,
                  validator: (text) {
                    // if (text.isEmpty && email.text.isEmpty) {
                    //   return 'Debes introducir al menos un email o teléfono';
                    // }
                    if (!isValidPhone(text)) {
                      return 'Introduce un teléfono de 9 cifras';
                    }
                    return null;
                  },
                  controller: phone,
                ),
                // TextFormField(
                //   decoration: const InputDecoration(
                //     icon: Icon(Icons.email),
                //     labelText: 'Dirección de email (opcional si hay teléfono)',
                //   ),
                //                   keyboardType: TextInputType.emailAddress,
                //   validator: (text) {
                //     if (text.isEmpty && phone.text.isEmpty) {
                //       return 'Debes introducir al menos un email o teléfono';
                //     }
                //     if (!text.isEmpty && !text.contains('@')) {
                //       return 'Debe tener una @';
                //     }
                //     return null;
                //   },
                //   controller: email,
                // ),
                TextFormField(
                  decoration: const InputDecoration(
                    icon: Icon(Icons.person),
                    labelText: 'Nombre (opcional)',
                  ),
                  controller: name,
                ),
                TextFormField(
                  decoration: const InputDecoration(
                    icon: Icon(Icons.comment),
                    labelText: 'Comentarios (opcional) (se envía al cliente)',
                  ),
                  keyboardType: TextInputType.multiline,
                  maxLines: null,
                  controller: comments,
                ),
                ButtonBar(
                  alignment: MainAxisAlignment.end,
                  children: [
                    FlatButton(
                      child: Text('CANCELAR'),
                      onPressed: () {
                        Navigator.pop(context);
                      },
                    ),
                    RaisedButton(
                      child: Text('CREAR CITA'),
                      onPressed: () {
                        if (!formKey.currentState.validate()) {
                          return;
                        }
                        submit(context);
                      },
                    ),
                  ],
                ),
              ],
            )),
          ),
        ),
      ));
}

extension on Duration {
  String formatZoneOffset() =>
      '${this > Duration.zero ? '+' : '-'}${inHours.abs().toString().padLeft(2, '0')}:${inMinutes.abs().remainder(60).toString().padLeft(2, '0')}';
}

extension on DateTime {
  String formatJson() =>
      '${toIso8601String()}${timeZoneOffset.formatZoneOffset()}';

  DateTime atBeginning() => DateTime(year, month, day);

  String formatDMY() =>
      '${day.toString().padLeft(2, '0')}/${month.toString().padLeft(2, '0')}/${year.toString().padLeft(4, '0')}';
}

DateTime tryParseDMY(String from) {
  final parts = from.split('/');
  if (parts.length != 3) {
    return null;
  }
  final day = int.tryParse(parts[0]);
  if (day == null) {
    return null;
  }
  final month = int.tryParse(parts[1]);
  if (month == null) {
    return null;
  }
  final year = int.tryParse(parts[2]);
  if (year == null) {
    return null;
  }
  return DateTime(year, month, day);
}

extension on TimeOfDay {
  String formatHM() =>
      '${hour.toString().padLeft(2, '0')}:${minute.toString().padLeft(2, '0')}';

  Duration sinceBeginningOfDay() {
    return Duration(hours: hour, minutes: minute);
  }
}

TimeOfDay tryParseHM(String from) {
  final parts = from.split(':');
  if (parts.length != 2) {
    return null;
  }
  final hours = int.tryParse(parts[0]);
  if (hours == null) {
    return null;
  }
  final minutes = int.tryParse(parts[1]);
  if (minutes == null) {
    return null;
  }
  return TimeOfDay(hour: hours, minute: minutes);
}

bool isValidPhone(String phone) {
  phone = phone.replaceAll('-', '');
  phone = phone.replaceAll(' ', '');
  phone = phone.replaceAll('(', '');
  phone = phone.replaceAll(')', '');
  phone = phone.replaceAll('.', '');
  return phoneRegexp.hasMatch(phone);
}

RegExp phoneRegexp = RegExp('^(\\+34)?[0-9]{9}\$');

RegExp appCodeRegexp = RegExp('^[0-9]{6}\$');

void unexpectedErrorFeedback(BuildContext context, dynamic e, dynamic s) {
  print('$e\n$s');
  Scaffold.of(context).removeCurrentSnackBar();
  Scaffold.of(context).showSnackBar(SnackBar(
      content: Text('Ha ocurrido un error inesperado. Inténtalo de nuevo')));
}

String whatsappURL(String phone, [String message]) {
  if (!phone.startsWith('+')) {
    phone = '+34$phone';
  }
  final urlMessage =
      message == null ? '' : '?text=${Uri.encodeComponent(message)}';
  return 'https://wa.me/${phone.replaceAll(RegExp(r'[^0-9]'), '')}$urlMessage';
}

String nameAndInitials(String name) {
  final parts = name.split(RegExp(' '));
  return ([parts[0]] + parts.sublist(1).map((e) => '${e[0]}.').toList())
      .join(' ');
}

extension on TimeOfDay {
  TimeOfDay add(Duration duration) {
    final added = this.sinceBeginningOfDay() + duration;
    return TimeOfDay(hour: added.inHours % 24, minute: added.inMinutes % 60);
  }
}
