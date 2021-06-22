// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'main.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

LoggedInBusiness _$LoggedInBusinessFromJson(Map<String, dynamic> json) {
  return LoggedInBusiness()
    ..authToken = json['authToken'] as String
    ..name = json['name'] as String
    ..email = json['email'] as String
    ..phone = json['phone'] as String
    ..photo = json['photo'] as String
    ..address = json['address'] as String;
}

Map<String, dynamic> _$LoggedInBusinessToJson(LoggedInBusiness instance) =>
    <String, dynamic>{
      'authToken': instance.authToken,
      'name': instance.name,
      'email': instance.email,
      'phone': instance.phone,
      'photo': instance.photo,
      'address': instance.address,
    };

Appointment _$AppointmentFromJson(Map<String, dynamic> json) {
  return Appointment()
    ..id = json['id'] as String
    ..number = json['number'] as int
    ..start =
        json['start'] == null ? null : DateTime.parse(json['start'] as String)
    ..end = json['end'] == null ? null : DateTime.parse(json['end'] as String)
    ..phone = json['phone'] as String
    ..email = json['email'] as String
    ..startedAt = json['startedAt'] == null
        ? null
        : DateTime.parse(json['startedAt'] as String)
    ..name = json['name'] as String;
}

Map<String, dynamic> _$AppointmentToJson(Appointment instance) =>
    <String, dynamic>{
      'id': instance.id,
      'number': instance.number,
      'start': instance.start?.toIso8601String(),
      'end': instance.end?.toIso8601String(),
      'phone': instance.phone,
      'email': instance.email,
      'startedAt': instance.startedAt?.toIso8601String(),
      'name': instance.name,
    };

ServerResponse _$ServerResponseFromJson(Map<String, dynamic> json) {
  return ServerResponse()
    ..result = json['result'] as String
    ..payload = json['payload'];
}

Map<String, dynamic> _$ServerResponseToJson(ServerResponse instance) =>
    <String, dynamic>{
      'result': instance.result,
      'payload': instance.payload,
    };
