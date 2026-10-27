package main

import (
	"fmt"
	"testing"

	"github.com/ypsu/efftesting/efft"
)

func TestDumpHash(t *testing.T) {
	efft.Init(t)
	efft.Effect(fmt.Sprintf("%016x", efft.Must1(dump()).Hash())).Equals("324e9289ed533ef8")
}
