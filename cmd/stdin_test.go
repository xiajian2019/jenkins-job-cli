package cmd

import (
	"os"
	"testing"
	"time"
)

func TestNewStdin(t *testing.T) {
	stdin := NewStdin()
	if stdin == nil {
		t.Error("NewStdin() 返回了空值")
	}
	if stdin.ch == nil {
		t.Error("NewStdin() 没有初始化通道")
	}
}

func TestJJStdin_NewListener(t *testing.T) {
	stdin := NewStdin()

	// 测试首次创建
	if stdin.ch == nil {
		t.Log("初始通道未创建")
	}

	// 测试重新创建
	oldCh := stdin.ch
	stdin.NewListener()
	if stdin.ch == oldCh {
		t.Error("NewListener() 没有创建新的通道")
	}
	t.Logf("stdin.ch: %+v", stdin.ch)
}

func TestJJStdin_Read(t *testing.T) {
	stdin := NewStdin()

	// 准备测试数据
	testData := []byte("test")
	buf := make([]byte, 1)

	// 模拟输入
	go func() {
		stdin.ch <- []byte{testData[0]}
	}()

	// 读取数据
	n, err := stdin.Read(buf)
	if err != nil {
		t.Errorf("Read() 返回错误: %v", err)
	}
	if n != 1 {
		t.Errorf("Read() 返回的长度错误，期望 1，实际 %d", n)
	}
	if buf[0] != testData[0] {
		t.Errorf("Read() 读取的数据错误，期望 %v，实际 %v", testData[0], buf[0])
	}
}

func TestJJStdin_Close(t *testing.T) {
	stdin := NewStdin()
	err := stdin.Close()
	if err != nil {
		t.Errorf("Close() 返回错误: %v", err)
	}
}

func TestJJStdin_HandleWithInput(t *testing.T) {
	// 保存原始的标准输入
	oldStdin := os.Stdin
	defer func() {
		os.Stdin = oldStdin
	}()

	// 创建模拟的输入
	r, w, _ := os.Pipe()
	os.Stdin = r

	stdin := NewStdin()

	// 写入测试数据
	go func() {
		w.Write([]byte("test"))
		w.Close()
	}()

	// 等待数据处理
	time.Sleep(100 * time.Millisecond)

	// 读取并验证数据
	buf := make([]byte, 1)
	n, err := stdin.Read(buf)
	if err != nil {
		t.Errorf("从处理的数据中读取时发生错误: %v", err)
	}
	if n != 1 || buf[0] != 't' {
		t.Errorf("处理的数据不正确，期望 't'，实际 %v", buf[0])
	}
}
