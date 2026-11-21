// autostart.go — 开机自启：写 HKCU\...\Run，指向当前 exe（带引号）。
// 启动后不自动开启遮罩，行为与手动双击一致。注册表是唯一事实来源，
// 面板显示以它为准（config.json 不存这个开关）。
package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "Veil"
)

func setAutoStart(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		k.DeleteValue(runValueName) // 本来就没开时忽略"找不到"错误
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(runValueName, fmt.Sprintf(`"%s"`, exe))
}

// autoStartEnabled 读注册表当前状态。指向别的 Veil 副本（便携/安装版互换）也算已开启。
func autoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValueName)
	return err == nil && v != ""
}
