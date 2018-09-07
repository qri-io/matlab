package matlab

import (
	"os"
	"testing"
)

func TestNewFileFromReader(t *testing.T) {
	qm7, err := os.Open("testdata/qm7.mat")
	if err != nil {
		t.Fatal(err.Error())
	}
	defer qm7.Close()

	f, err := NewFileFromReader(qm7)
	if err != nil {
		t.Log(f.Header.String())
		t.Fatal(err.Error())
	}

	expect := "MATLAB 5.0 MAT-file, Platform: posix, Created on: Mon Feb 18 17:12:08 2013"
	if f.Header.String() != expect {
		t.Errorf("header mismatch. expected:\n%s\ngot:\n%s", expect, f.Header.String())
	}
}

func TestReadElement(t *testing.T) {
	qm7, err := os.Open("testdata/varTypes.mat")
	if err != nil {
		t.Fatal(err.Error())
	}
	defer qm7.Close()

	f, err := NewFileFromReader(qm7)
	if err != nil {
		t.Log(f.Header.String())
		t.Fatal(err.Error())
	}

	_, err = f.ReadElement()
	if err != nil {
		t.Fatal(err.Error())
	}
}
