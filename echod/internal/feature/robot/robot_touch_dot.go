//go:build dot

package robot

func lensCovered() bool { return false }

func listenLensCover(func()) func() { return func() {} }
