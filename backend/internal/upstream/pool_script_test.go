package upstream

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"hstore/backend/internal/store"
)

// 升级脚本必须能被 shell 正确解析（曾因 Go 字符串拼接漏进脚本体，
// 生成 `echo "..." + fmt.Sprint(1) + "..."` 导致语法错误）。
func TestBuildUpdateScriptIsValidShell(t *testing.T) {
	node := store.PoolNode{Index: 1, Label: "测试", Token: "tok"}
	script := BuildUpdateScript("example.com:3001", node)

	// 语法检查：bash -n 只解析不执行
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("无 bash，跳过语法检查")
	}
	f, err := os.CreateTemp("", "update-*.sh")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(script); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	out, err := exec.Command("bash", "-n", f.Name()).CombinedOutput()
	if err != nil {
		t.Fatalf("脚本语法错误: %v\n%s\n--- 脚本 ---\n%s", err, out, script)
	}
	// 不应残留 Go 侧的拼接痕迹
	if strings.Contains(script, "+ fmt.Sprint") {
		t.Fatalf("脚本里残留 Go 拼接代码:\n%s", script)
	}
	if !strings.Contains(script, "NODE_IDX=1") {
		t.Fatalf("缺少 NODE_IDX 变量:\n%s", script)
	}
}

// 安装脚本同样做一次语法检查。
func TestBuildOnboardScriptIsValidShell(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("无 bash")
	}
	node := store.PoolNode{Index: 2, Label: "测试", Token: "tok"}
	script := BuildOnboardScript("example.com:3001", node)
	f, err := os.CreateTemp("", "install-*.sh")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(script); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if out, err := exec.Command("bash", "-n", f.Name()).CombinedOutput(); err != nil {
		t.Fatalf("安装脚本语法错误: %v\n%s", err, out)
	}
}
