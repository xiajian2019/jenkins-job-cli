package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/chzyer/readline"
	"github.com/spf13/cobra"
)

func init() {
    k8sCmd := &cobra.Command{
        Use:   "k8s",
        Short: "Kubernetes相关操作",
        Long:  `管理和监控Kubernetes资源`,
    }

    // Pod状态查看命令
    var namespace string
    var watch bool
    var selector string
    var showLogs bool
    var follow bool
    var detailed bool
    var simple bool

    podsCmd := &cobra.Command{
        Use:   "pods [app-name]",
        Short: "查看Pod状态和日志",
        Long: `查看Kubernetes Pod的详细状态信息和日志

示例:
  jj k8s pods                    # 查看所有Pod (简洁模式)
  jj k8s pods myapp              # 模糊匹配包含myapp的Pod，支持选择
  jj k8s pods -l service=web     # 使用自定义标签选择器
  jj k8s pods myapp -w           # 实时监控Pod状态
  jj k8s pods myapp --logs       # 查看Pod最近100行日志并实时追踪
  jj k8s pods myapp --logs --no-follow  # 仅查看最近100行日志，不追踪
  jj k8s pods myapp -d           # 显示详细信息
  jj k8s pods myapp -s           # 简洁模式 (仅显示基本状态)`,
        Run: func(cmd *cobra.Command, args []string) {
            showPodStatus(args, namespace, selector, watch, showLogs, follow, detailed, simple)
        },
    }

    // 为pods命令添加参数
    podsCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes命名空间")
    podsCmd.Flags().StringVarP(&selector, "selector", "l", "", "标签选择器 (例如: app=myapp,version=v1)")
    podsCmd.Flags().BoolVarP(&watch, "watch", "w", false, "实时监控Pod状态变化")
    podsCmd.Flags().BoolVar(&showLogs, "logs", false, "显示Pod日志 (默认最近100行并实时追踪)")
    podsCmd.Flags().BoolVar(&follow, "no-follow", false, "禁用实时追踪日志 (仅在--logs时有效)")
    podsCmd.Flags().BoolVarP(&detailed, "detailed", "d", false, "显示Pod详细信息")
    podsCmd.Flags().BoolVarP(&simple, "simple", "s", false, "简洁模式，仅显示基本状态")

    k8sCmd.AddCommand(podsCmd)
    rootCmd.AddCommand(k8sCmd)
}

func showPodStatus(args []string, namespace, selector string, watch, showLogs, noFollow, detailed, simple bool) {
    var labelSelector string
    var selectedPods []string

    // 构建标签选择器或进行模糊匹配
    if selector != "" {
        labelSelector = selector
    } else if len(args) > 0 {
        // 进行模糊匹配
        matchedPods := findMatchingPods(args[0], namespace)
        if len(matchedPods) == 0 {
            // 如果没有找到匹配的Pod，尝试作为标签选择器
            labelSelector = fmt.Sprintf("app=%s", args[0])
        } else if len(matchedPods) == 1 {
            // 只有一个匹配，直接使用
            selectedPods = matchedPods
        } else {
            // 多个匹配，让用户选择
            selectedPod := selectPodFromList(matchedPods, args[0])
            if selectedPod != "" {
                selectedPods = []string{selectedPod}
            } else {
                return // 用户取消选择
            }
        }
    }

    if showLogs {
        // --logs 默认开启实时追踪，除非指定了 --no-follow
        follow := !noFollow
        if len(selectedPods) > 0 {
            showPodLogsByName(selectedPods[0], namespace, follow)
        } else {
            showPodLogs(args, namespace, labelSelector, follow)
        }
        return
    }

    if watch {
        if len(selectedPods) > 0 {
            watchSpecificPods(selectedPods, namespace)
        } else {
            watchPodStatus(namespace, labelSelector)
        }
        return
    }

    // 一次性查看Pod状态
    if len(selectedPods) > 0 {
        showSpecificPods(selectedPods, namespace, detailed)
    } else {
        if simple {
            getPodStatusSimple(namespace, labelSelector)
        } else if detailed {
            getPodStatusDetailed(namespace, labelSelector, true)
        } else {
            // 默认模式：显示基本信息，不显示详细信息
            getPodStatusDetailed(namespace, labelSelector, false)
        }
    }
}

// 模糊匹配Pod名称
func findMatchingPods(pattern, namespace string) []string {
    // 获取所有Pod
    cmd := exec.Command("kubectl", "get", "pods", "-n", namespace, "--no-headers", "-o", "custom-columns=NAME:.metadata.name")
    output, err := cmd.Output()
    if err != nil {
        return nil
    }

    allPods := strings.Split(strings.TrimSpace(string(output)), "\n")
    var matchedPods []string

    pattern = strings.ToLower(pattern)
    for _, pod := range allPods {
        if pod == "" {
            continue
        }
        if strings.Contains(strings.ToLower(pod), pattern) {
            matchedPods = append(matchedPods, pod)
        }
    }

    return matchedPods
}

// 让用户从Pod列表中选择
func selectPodFromList(pods []string, pattern string) string {
    fmt.Printf("\n🔍 找到 %d 个匹配 '%s' 的Pod:\n", len(pods), pattern)
    for i, pod := range pods {
        fmt.Printf("%d. %s\n", i+1, pod)
    }

    rl, err := readline.New("\n请选择要操作的Pod编号 (按Enter取消): ")
    if err != nil {
        fmt.Printf("读取输入失败: %v\n", err)
        return ""
    }
    defer rl.Close()

    line, err := rl.Readline()
    if err != nil {
        return ""
    }

    line = strings.TrimSpace(line)
    if line == "" {
        fmt.Println("已取消选择")
        return ""
    }

    index, err := strconv.Atoi(line)
    if err != nil || index < 1 || index > len(pods) {
        fmt.Println("无效的选择")
        return ""
    }

    selectedPod := pods[index-1]
    fmt.Printf("✅ 已选择Pod: %s\n\n", selectedPod)
    return selectedPod
}

// 显示特定的Pod
func showSpecificPods(podNames []string, namespace string, detailed bool) {
    fmt.Printf("📊 Pod状态 (命名空间: %s):\n", namespace)

    for _, podName := range podNames {
        cmd := exec.Command("kubectl", "get", "pod", podName, "-n", namespace, "-o", "wide")
        output, err := cmd.Output()
        if err != nil {
            fmt.Printf("❌ 获取Pod %s 状态失败: %v\n", podName, err)
            continue
        }
        fmt.Printf("%s", output)
    }

    if detailed {
        fmt.Printf("\n📋 Pod详细信息:\n")
        for _, podName := range podNames {
            showSinglePodDetails(podName, namespace)
        }
    }
}

// 显示单个Pod的详细信息
func showSinglePodDetails(podName, namespace string) {
    fmt.Printf("\n🔸 Pod: %s\n", podName)

    cmd := exec.Command("kubectl", "describe", "pod", podName, "-n", namespace)
    output, err := cmd.Output()
    if err != nil {
        fmt.Printf("  ❌ 获取详细信息失败: %v\n", err)
        return
    }

    // 提取关键信息
    lines := strings.Split(string(output), "\n")
    for _, line := range lines {
        line = strings.TrimSpace(line)
        if strings.HasPrefix(line, "Status:") ||
            strings.HasPrefix(line, "Ready:") ||
            strings.HasPrefix(line, "Restarts:") ||
            strings.HasPrefix(line, "Age:") ||
            strings.HasPrefix(line, "Node:") ||
            strings.HasPrefix(line, "IP:") ||
            strings.Contains(line, "Warning") ||
            strings.Contains(line, "Error") {
            fmt.Printf("  %s\n", line)
        }
    }
}

// 监控特定的Pod
func watchSpecificPods(podNames []string, namespace string) {
    fmt.Printf("👀 实时监控Pod状态 (命名空间: %s)\n", namespace)
    fmt.Printf("📋 监控Pod: %s\n", strings.Join(podNames, ", "))
    fmt.Printf("按 Ctrl+C 退出监控\n\n")

    // 设置信号处理，捕获 Ctrl+C
    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    
    // 创建一个用于停止监控的通道
    stopChan := make(chan bool)
    
    // 启动信号监听协程
    go func() {
        <-c
        fmt.Printf("\n\n👋 收到退出信号，停止监控...\n")
        stopChan <- true
    }()

    for {
        select {
        case <-stopChan:
            return
        default:
            fmt.Printf("\r⏰ %s - 检查Pod状态...\n", time.Now().Format("15:04:05"))

            runningCount := 0
            totalCount := len(podNames)

            for _, podName := range podNames {
                cmd := exec.Command("kubectl", "get", "pod", podName, "-n", namespace, "--no-headers")
                output, err := cmd.Output()
                if err != nil {
                    fmt.Printf("❌ %s: 获取状态失败 - %v\n", podName, err)
                    continue
                }

                line := strings.TrimSpace(string(output))
                if line == "" {
                    fmt.Printf("⚠️  %s: Pod不存在\n", podName)
                    continue
                }

                fields := strings.Fields(line)
                if len(fields) >= 3 {
                    ready := fields[1]
                    status := fields[2]

                    if status == "Running" && strings.Contains(ready, "/") {
                        readyParts := strings.Split(ready, "/")
                        if len(readyParts) == 2 && readyParts[0] == readyParts[1] {
                            runningCount++
                            fmt.Printf("✅ %s: %s (%s)\n", podName, status, ready)
                        } else {
                            fmt.Printf("⚠️  %s: %s (%s) - 未完全就绪\n", podName, status, ready)
                        }
                    } else {
                        fmt.Printf("❌ %s: %s (%s)\n", podName, status, ready)
                    }
                }
            }

            fmt.Printf("\n📊 总计: %d/%d Pod运行正常\n", runningCount, totalCount)

            if runningCount == totalCount && totalCount > 0 {
                fmt.Printf("🎉 所有Pod都已正常运行！\n")
                // 继续监控，不退出
            }

            fmt.Printf("\n" + strings.Repeat("-", 50) + "\n")
            time.Sleep(5 * time.Second)
        }
    }
}

// 通过Pod名称查看日志 (改进版本)
func showPodLogsByName(podName, namespace string, follow bool) {
    fmt.Printf("📜 查看Pod日志: %s (命名空间: %s)\n", podName, namespace)
    if follow {
        fmt.Printf("🔄 显示最近100行日志并实时追踪 (按 Ctrl+C 退出)\n")
    } else {
        fmt.Printf("📋 显示最近100行日志\n")
    }
    fmt.Printf("\n" + strings.Repeat("-", 50) + "\n")

    // 构建kubectl logs命令，默认获取最近100行
    cmdArgs := []string{"logs", podName, "-n", namespace, "--tail=100"}
    if follow {
        cmdArgs = append(cmdArgs, "-f")
    }

    cmd := exec.Command("kubectl", cmdArgs...)
    
    if follow {
        // 实时追踪模式：直接连接到stdout/stderr，支持Ctrl+C中断
        cmd.Stdout = os.Stdout
        cmd.Stderr = os.Stderr
        
        // 设置信号处理
        c := make(chan os.Signal, 1)
        signal.Notify(c, os.Interrupt, syscall.SIGTERM)
        
        // 启动命令
        err := cmd.Start()
        if err != nil {
            fmt.Printf("❌ 启动日志追踪失败: %v\n", err)
            return
        }
        
        // 等待信号或命令完成
        go func() {
            <-c
            if cmd.Process != nil {
                cmd.Process.Kill()
            }
        }()
        
        err = cmd.Wait()
        if err != nil && !strings.Contains(err.Error(), "killed") {
            fmt.Printf("\n❌ 日志追踪中断: %v\n", err)
        } else {
            fmt.Printf("\n👋 日志追踪已停止\n")
        }
    } else {
        // 一次性获取模式
        output, err := cmd.CombinedOutput()
        if err != nil {
            fmt.Printf("❌ 获取日志失败: %v\n", err)
            return
        }
        fmt.Printf("%s", output)
    }
}

func getPodStatusSimple(namespace, labelSelector string) {
    // 构建kubectl命令
    args := []string{"get", "pods"}
    if labelSelector != "" {
        args = append(args, "-l", labelSelector)
    }
    args = append(args, "-n", namespace)

    cmd := exec.Command("kubectl", args...)
    output, err := cmd.Output()
    if err != nil {
        fmt.Printf("❌ 获取Pod状态失败: %v\n", err)
        return
    }

    if len(strings.TrimSpace(string(output))) == 0 {
        fmt.Printf("⚠️  未找到匹配的Pod\n")
        return
    }

    fmt.Printf("%s", output)
}

func getPodStatusDetailed(namespace, labelSelector string, showDetails bool) {
    if !showDetails {
        // 简洁模式，不显示额外信息
        fmt.Printf("📊 Pod状态 (命名空间: %s):\n", namespace)
    } else {
        fmt.Printf("🔍 查看Pod状态 (命名空间: %s)\n", namespace)
        if labelSelector != "" {
            fmt.Printf("📋 标签选择器: %s\n", labelSelector)
        }
        fmt.Println()
    }

    // 构建kubectl命令
    args := []string{"get", "pods"}
    if labelSelector != "" {
        args = append(args, "-l", labelSelector)
    }
    args = append(args, "-n", namespace, "-o", "wide")

    cmd := exec.Command("kubectl", args...)
    output, err := cmd.Output()
    if err != nil {
        fmt.Printf("❌ 获取Pod状态失败: %v\n", err)

        // 尝试不同的标签选择器
        if labelSelector != "" && strings.Contains(labelSelector, "app=") {
            tryAlternativeSelectors(namespace, labelSelector)
        }
        return
    }

    if len(strings.TrimSpace(string(output))) == 0 {
        fmt.Printf("⚠️  未找到匹配的Pod\n")
        if labelSelector != "" && showDetails {
            fmt.Printf("💡 提示: 尝试使用 'jj k8s pods' 查看所有Pod，或使用不同的标签选择器\n")
        }
        return
    }

    fmt.Printf("%s\n", output)

    // 只有在详细模式下才显示额外的详细信息
    if showDetails {
        showPodDetails(namespace, labelSelector)
    }
}

func showPodDetails(namespace, labelSelector string) {
    // 获取Pod名称列表
    args := []string{"get", "pods"}
    if labelSelector != "" {
        args = append(args, "-l", labelSelector)
    }
    args = append(args, "-n", namespace, "--no-headers", "-o", "custom-columns=NAME:.metadata.name")

    cmd := exec.Command("kubectl", args...)
    output, err := cmd.Output()
    if err != nil {
        return
    }

    podNames := strings.Split(strings.TrimSpace(string(output)), "\n")
    if len(podNames) == 0 || podNames[0] == "" {
        return
    }

    fmt.Printf("\n📋 Pod详细信息:\n")
    for _, podName := range podNames {
        if podName == "" {
            continue
        }
        showSinglePodDetails(podName, namespace)
    }
}

func tryAlternativeSelectors(namespace, originalSelector string) {
    appName := strings.TrimPrefix(originalSelector, "app=")
    alternatives := []string{
        fmt.Sprintf("app.kubernetes.io/name=%s", appName),
        fmt.Sprintf("name=%s", appName),
        fmt.Sprintf("service=%s", appName),
        fmt.Sprintf("component=%s", appName),
    }

    fmt.Printf("🔄 尝试其他标签选择器...\n")
    for _, alt := range alternatives {
        cmd := exec.Command("kubectl", "get", "pods", "-l", alt, "-n", namespace, "--no-headers")
        output, err := cmd.Output()
        if err == nil && len(strings.TrimSpace(string(output))) > 0 {
            fmt.Printf("✅ 找到匹配的Pod (标签: %s):\n", alt)
            cmd = exec.Command("kubectl", "get", "pods", "-l", alt, "-n", namespace, "-o", "wide")
            output, _ = cmd.Output()
            fmt.Printf("%s\n", output)
            return
        }
    }
    fmt.Printf("❌ 未找到匹配的Pod\n")
}

func watchPodStatus(namespace, labelSelector string) {
    fmt.Printf("👀 实时监控Pod状态 (命名空间: %s)\n", namespace)
    if labelSelector != "" {
        fmt.Printf("📋 标签选择器: %s\n", labelSelector)
    }
    fmt.Printf("按 Ctrl+C 退出监控\n\n")

    // 设置信号处理，捕获 Ctrl+C
    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    
    // 创建一个用于停止监控的通道
    stopChan := make(chan bool)
    
    // 启动信号监听协程
    go func() {
        <-c
        fmt.Printf("\n\n👋 收到退出信号，停止监控...\n")
        stopChan <- true
    }()

    for {
        select {
        case <-stopChan:
            return
        default:
            fmt.Printf("\r⏰ %s - 检查Pod状态...\n", time.Now().Format("15:04:05"))

            args := []string{"get", "pods"}
            if labelSelector != "" {
                args = append(args, "-l", labelSelector)
            }
            args = append(args, "-n", namespace, "--no-headers")

            cmd := exec.Command("kubectl", args...)
            output, err := cmd.Output()
            if err != nil {
                fmt.Printf("❌ 监控失败: %v\n", err)
                time.Sleep(5 * time.Second)
                continue
            }

            lines := strings.Split(strings.TrimSpace(string(output)), "\n")
            if len(lines) == 0 || lines[0] == "" {
                fmt.Printf("⚠️  未找到匹配的Pod\n")
            } else {
                runningCount := 0
                totalCount := 0

                for _, line := range lines {
                    if line == "" {
                        continue
                    }
                    totalCount++
                    fields := strings.Fields(line)
                    if len(fields) >= 3 {
                        podName := fields[0]
                        ready := fields[1]
                        status := fields[2]

                        if status == "Running" && strings.Contains(ready, "/") {
                            readyParts := strings.Split(ready, "/")
                            if len(readyParts) == 2 && readyParts[0] == readyParts[1] {
                                runningCount++
                                fmt.Printf("✅ %s: %s (%s)\n", podName, status, ready)
                            } else {
                                fmt.Printf("⚠️  %s: %s (%s) - 未完全就绪\n", podName, status, ready)
                            }
                        } else {
                            fmt.Printf("❌ %s: %s (%s)\n", podName, status, ready)
                        }
                    }
                }

                fmt.Printf("\n📊 总计: %d/%d Pod运行正常\n", runningCount, totalCount)

                if runningCount == totalCount && totalCount > 0 {
                    fmt.Printf("🎉 所有Pod都已正常运行！\n")
                    // 继续监控，不退出
                }
            }

            fmt.Printf("\n" + strings.Repeat("-", 50) + "\n")
            time.Sleep(5 * time.Second)
        }
    }
}

func showPodLogs(args []string, namespace, labelSelector string, follow bool) {
    var podName string

    if len(args) > 0 && !strings.Contains(args[0], "=") {
        // 如果参数不包含=，可能是直接的Pod名称
        podName = args[0]
    } else {
        // 通过标签选择器获取Pod名称
        cmdArgs := []string{"get", "pods"}
        if labelSelector != "" {
            cmdArgs = append(cmdArgs, "-l", labelSelector)
        }
        cmdArgs = append(cmdArgs, "-n", namespace, "--no-headers", "-o", "custom-columns=NAME:.metadata.name")

        cmd := exec.Command("kubectl", cmdArgs...)
        output, err := cmd.Output()
        if err != nil {
            fmt.Printf("❌ 获取Pod名称失败: %v\n", err)
            return
        }

        podNames := strings.Split(strings.TrimSpace(string(output)), "\n")
        if len(podNames) == 0 || podNames[0] == "" {
            fmt.Printf("⚠️  未找到匹配的Pod\n")
            return
        }

        podName = podNames[0] // 使用第一个Pod
        if len(podNames) > 1 {
            fmt.Printf("📋 找到多个Pod，显示第一个: %s\n", podName)
        }
    }

    showPodLogsByName(podName, namespace, follow)
}