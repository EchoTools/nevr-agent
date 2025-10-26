package main

import (
	"reflect"
	"testing"
)

func Test_parsePortRange(t *testing.T) {
	type args struct {
		port string
	}
	tests := []struct {
		name    string
		args    args
		want    []int
		wantErr bool
	}{
		{
			name:    "Single port",
			args:    args{port: "80"},
			want:    []int{80},
			wantErr: false,
		},
		{
			name:    "Port range",
			args:    args{port: "80-90"},
			want:    []int{80, 81, 82, 83, 84, 85, 86, 87, 88, 89, 90},
			wantErr: false,
		},
		{
			name:    "Invalid port: not a number",
			args:    args{port: "a"},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Invalid port range: two dashes",
			args:    args{port: "80-90-100"},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Invalid port range: missing second part",
			args:    args{port: "80-"},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Invalid port range: negative port",
			args:    args{port: "-90"},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "Invalid port range: starting port higher than ending port",
			args:    args{port: "90-80"},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "single length port range",
			args:    args{port: "80-80"},
			want:    []int{80},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePortRange(tt.args.port)
			if (err != nil) != tt.wantErr {
				t.Errorf("parsePortRange() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsePortRange() = %v, want %v", got, tt.want)
			}
		})
	}
}
