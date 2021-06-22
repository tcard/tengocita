package main

import (
	"context"
	"fmt"
	golog "log"
)

type logCtxKey struct{}

type logger interface {
	Printf(format string, v ...interface{})
}

type logFunc func(format string, v ...interface{})

func (f logFunc) Printf(format string, v ...interface{}) {
	f(format, v...)
}

func log(ctx context.Context) logger {
	var scopes string
	kv, _ := ctx.Value(logCtxKey{}).(*logScope)
	for kv != nil {
		scopes = fmt.Sprintf("%s=%v ", kv.k, kv.v) + scopes
		kv = kv.prev
	}
	return logFunc(func(format string, v ...interface{}) {
		golog.Printf(scopes+format, v...)
	})
}

func scope(ctx context.Context, k string, v interface{}) context.Context {
	kv, _ := ctx.Value(logCtxKey{}).(*logScope)
	return context.WithValue(ctx, logCtxKey{}, kv.prepend(k, v))
}

type logScope struct {
	prev *logScope
	k    string
	v    interface{}
}

func (s *logScope) prepend(k string, v interface{}) *logScope {
	return &logScope{
		prev: s,
		k:    k,
		v:    v,
	}
}
