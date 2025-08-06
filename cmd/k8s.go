package cmd

import (
	"context"
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
	// Pod状态查看命令的参数
	var namespace string
	var watch bool
	var selector string
	var showLogs bool
	var follow bool
	var detailed bool
	var simple bool
	var execContainer bool

	k8sCmd := &cobra.Command{
		Use:   "k8s [app-name]",
		Short: "Kubernetes相关操作",
		Long: `管理和监控Kubernetes资源

默认行为等同于 pods 子命令，查看Pod状态和日志

示例:
  jj k8s                         # 查看所有Pod (简洁模式)
  jj k8s myapp                   # 模糊匹配包含myapp的Pod，支持选择
  jj k8s myapp -w                # 实时监控Pod状态
  jj k8s myapp -l                # 查看Pod最近100行日志并实时追踪
  jj k8s myapp -l --no-follow    # 仅查看最近100行日志，不追踪
  jj k8s myapp -d                # 显示详细信息
  jj k8s myapp -e                # 进入容器交互式终端
  jj k8s myapp -s                # 简洁模式 (仅显示基本状态)
  
  jj k8s pods myapp              # 等同于 jj k8s myapp (显式使用pods子命令)`,
		Run: func(cmd *cobra.Command, args []string) {
			// 默认执行 pods 逻辑
			showPodStatus(args, namespace, selector, watch, showLogs, follow, detailed, simple, execContainer)
		},
	}

	// 为k8s主命令添加参数
	k8sCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes命名空间")
	k8sCmd.Flags().BoolVarP(&watch, "watch", "w", false, "实时监控Pod状态变化")
	k8sCmd.Flags().BoolVarP(&showLogs, "log", "l", false, "显示Pod日志 (默认最近100行并实时追踪)")
	k8sCmd.Flags().BoolVar(&follow, "no-follow", false, "禁用实时追踪日志 (仅在--logs时有效)")
	k8sCmd.Flags().BoolVarP(&detailed, "detailed", "d", false, "显示Pod详细信息")
	k8sCmd.Flags().BoolVarP(&simple, "simple", "s", false, "简洁模式，仅显示基本状态")
	k8sCmd.Flags().BoolVarP(&execContainer, "exec", "e", false, "进入容器交互式终端")

	podsCmd := &cobra.Command{
		Use:   "pods [app-name]",
		Short: "查看Pod状态和日志",
		Long: `查看Kubernetes Pod的详细状态信息和日志

示例:
  jj k8s pods                    # 查看所有Pod (简洁模式)
  jj k8s pods myapp              # 模糊匹配包含myapp的Pod，支持选择
  jj k8s pods myapp -w           # 实时监控Pod状态
  jj k8s pods myapp -l           # 查看Pod最近100行日志并实时追踪
  jj k8s pods myapp -l --no-follow  # 仅查看最近100行日志，不追踪
  jj k8s pods myapp -d           # 显示详细信息
  jj k8s pods myapp -e           # 进入容器交互式终端
  jj k8s pods myapp -s           # 简洁模式 (仅显示基本状态)`,
		Run: func(cmd *cobra.Command, args []string) {
			showPodStatus(args, namespace, selector, watch, showLogs, follow, detailed, simple, execContainer)
		},
	}

	// 为pods命令添加参数（继承父命令的参数）
	podsCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes命名空间")
	podsCmd.Flags().BoolVarP(&watch, "watch", "w", false, "实时监控Pod状态变化")
	podsCmd.Flags().BoolVarP(&showLogs, "log", "l", false, "显示Pod日志 (默认最近100行并实时追踪)")
	podsCmd.Flags().BoolVar(&follow, "no-follow", false, "禁用实时追踪日志 (仅在--logs时有效)")
	podsCmd.Flags().BoolVarP(&detailed, "detailed", "d", false, "显示Pod详细信息")
	podsCmd.Flags().BoolVarP(&simple, "simple", "s", false, "简洁模式，仅显示基本状态")
	podsCmd.Flags().BoolVarP(&execContainer, "exec", "e", false, "进入容器交互式终端")

	k8sCmd.AddCommand(podsCmd)
	rootCmd.AddCommand(k8sCmd)
}

// 在 showPodStatus 函数中添加 execContainer 参数
func showPodStatus(args []string, namespace, selector string, watch, showLogs, noFollow, detailed, simple, execContainer bool) {
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
			selectedPods = selectPodFromList(matchedPods, args[0])
			if len(selectedPods) == 0 {
				return // 用户取消选择
			}
		}
	}

	if showLogs {
		// --logs 默认开启实时追踪，除非指定了 --no-follow
		follow := !noFollow
		if len(selectedPods) > 0 {
			// 如果选择了多个Pod，显示第一个Pod的日志
			// 也可以考虑让用户再次选择一个Pod来查看日志
			if len(selectedPods) > 1 {
				fmt.Printf("⚠️  选择了多个Pod，将显示第一个Pod (%s) 的日志\n", selectedPods[0])
			}
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

	if execContainer {
		if len(selectedPods) > 0 {
			// 如果选择了多个Pod，让用户再次选择一个Pod来进入
			if len(selectedPods) > 1 {
				fmt.Printf("⚠️  选择了多个Pod，请选择要进入的Pod:\n")
				for i, pod := range selectedPods {
					fmt.Printf("%d. %s\n", i+1, pod)
				}

				rl, err := readline.New("请选择要进入的Pod编号: ")
				if err != nil {
					fmt.Printf("读取输入失败: %v\n", err)
					return
				}
				defer rl.Close()

				line, err := rl.Readline()
				if err != nil {
					return
				}

				line = strings.TrimSpace(line)
				if line == "" {
					fmt.Println("已取消选择")
					return
				}

				index, err := strconv.Atoi(line)
				if err != nil || index < 1 || index > len(selectedPods) {
					fmt.Println("无效的选择")
					return
				}

				execPodContainer(selectedPods[index-1], namespace)
			} else {
				execPodContainer(selectedPods[0], namespace)
			}
		} else if len(args) > 0 {
			// 如果没有匹配的Pod，尝试直接使用输入的名称
			execPodContainer(args[0], namespace)
		} else {
			fmt.Println("❌ 请指定要进入的Pod名称")
		}
		return
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
		if strings.HasPrefix(strings.ToLower(pod), pattern) {
			matchedPods = append(matchedPods, pod)
		}
	}

	return matchedPods
}

// 让用户从Pod列表中选择（支持多选）
func selectPodFromList(pods []string, pattern string) []string {
	fmt.Printf("\n🔍 找到 %d 个匹配 '%s' 的Pod:\n", len(pods), pattern)
	for i, pod := range pods {
		fmt.Printf("%d. %s\n", i+1, pod)
	}

	rl, err := readline.New("\n请选择要操作的Pod编号 (多个用逗号或空格分隔，如: 1,3,5 或 1 3 5 或 按Enter取消): ")
	if err != nil {
		fmt.Printf("读取输入失败: %v\n", err)
		return nil
	}
	defer rl.Close()

	line, err := rl.Readline()
	if err != nil {
		return nil
	}

	line = strings.TrimSpace(line)
	if line == "" {
		fmt.Println("已取消选择")
		return nil
	}

	// 解析输入的编号，支持逗号和空格分割
	var selectedPods []string
	var indices []string

	// 先按逗号分割，再按空格分割
	if strings.Contains(line, ",") {
		indices = strings.Split(line, ",")
	} else {
		indices = strings.Fields(line)
	}

	for _, indexStr := range indices {
		indexStr = strings.TrimSpace(indexStr)
		if indexStr == "" {
			continue
		}

		index, err := strconv.Atoi(indexStr)
		if err != nil || index < 1 || index > len(pods) {
			continue
		}

		selectedPods = append(selectedPods, pods[index-1])
	}

	if len(selectedPods) == 0 {
		fmt.Println("未选择任何Pod")
		return nil
	}

	fmt.Printf("✅ 已选择Pod: %s\n\n", strings.Join(selectedPods, ", "))
	return selectedPods
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
		fmt.Printf("%s\n", line)
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
	defer signal.Stop(c)

	// 创建一个用于停止监控的通道
	stopChan := make(chan bool, 1)

	// 启动信号监听协程
	go func() {
		<-c
		fmt.Printf("\n\n👋 收到退出信号，停止监控...\n")
		select {
		case stopChan <- true:
		default:
		}
	}()

	// 创建定时器，用于替代 time.Sleep
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// 立即执行一次检查
	checkPodStatus := func() {
		fmt.Printf("\r⏰ %s - 检查Pod状态...\n", time.Now().Format("15:04:05"))

		for _, podName := range podNames {
			// 使用带超时的 context 来避免 kubectl 命令卡住
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			cmd := exec.CommandContext(ctx, "kubectl", "get", "pod", podName, "-n", namespace, "--no-headers")
			output, err := cmd.Output()
			cancel()

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
				age := ""
				if len(fields) >= 5 {
					age = fields[4]
				}

				if status == "Running" && strings.Contains(ready, "/") {
					readyParts := strings.Split(ready, "/")
					if len(readyParts) == 2 && readyParts[0] == readyParts[1] {
						fmt.Printf("✅ %s: %s (%s, Age: %s)\n", podName, status, ready, age)
					} else {
						fmt.Printf("⚠️  %s: %s (%s, Age: %s) - 未完全就绪\n", podName, status, ready, age)
					}
				} else {
					fmt.Printf("❌ %s: %s (%s, Age: %s)\n", podName, status, ready, age)
				}
			}
		}
		fmt.Printf("\n" + strings.Repeat("-", 50) + "\n")
	}

	// 立即执行一次检查
	checkPodStatus()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			checkPodStatus()
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
	defer signal.Stop(c)

	// 创建一个用于停止监控的通道
	stopChan := make(chan bool, 1)

	// 启动信号监听协程
	go func() {
		<-c
		fmt.Printf("\n\n👋 收到退出信号，停止监控...\n")
		select {
		case stopChan <- true:
		default:
		}
	}()

	// 创建定时器，用于替代 time.Sleep
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	checkPodStatus := func() {
		fmt.Printf("\r⏰ %s - 检查Pod状态...\n", time.Now().Format("15:04:05"))

		args := []string{"get", "pods"}
		if labelSelector != "" {
			args = append(args, "-l", labelSelector)
		}
		args = append(args, "-n", namespace, "--no-headers")

		// 使用带超时的 context
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, "kubectl", args...)
		output, err := cmd.Output()
		cancel()

		if err != nil {
			fmt.Printf("❌ 监控失败: %v\n", err)
			return
		}

		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		if len(lines) == 0 || lines[0] == "" {
			fmt.Printf("⚠️  未找到匹配的Pod\n")
		} else {
			for _, line := range lines {
				if line == "" {
					continue
				}
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					podName := fields[0]
					ready := fields[1]
					status := fields[2]
					age := ""
					if len(fields) >= 5 {
						age = fields[4]
					}

					if status == "Running" && strings.Contains(ready, "/") {
						readyParts := strings.Split(ready, "/")
						if len(readyParts) == 2 && readyParts[0] == readyParts[1] {
							fmt.Printf("✅ %s: %s (%s, Age: %s)\n", podName, status, ready, age)
						} else {
							fmt.Printf("⚠️  %s: %s (%s, Age: %s) - 未完全就绪\n", podName, status, ready, age)
						}
					} else {
						fmt.Printf("❌ %s: %s (%s, Age: %s)\n", podName, status, ready, age)
					}
				}
			}
		}
		fmt.Printf("\n" + strings.Repeat("-", 50) + "\n")
	}

	// 立即执行一次检查
	checkPodStatus()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			checkPodStatus()
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

func execPodContainer(podName, namespace string) {
	// 创建上下文用于控制子进程
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 检查Pod是否存在且运行正常
	cmd := exec.CommandContext(ctx, "kubectl", "get", "pod", podName, "-n", namespace, "--no-headers")
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("❌ Pod %s 不存在或无法访问\n", podName)
		return
	}

	// 检查Pod状态
	fields := strings.Fields(string(output))
	if len(fields) < 3 || fields[2] != "Running" {
		fmt.Printf("❌ Pod %s 未在运行状态 (当前状态: %s)\n", podName, fields[2])
		return
	}

	fmt.Printf("✅ 正在进入 Pod %s...\n", podName)

	// 准备进入容器的命令
	execCmd := exec.CommandContext(ctx, "kubectl", "exec", "-it", "-n", namespace, podName, "--", "/bin/sh")

	// 设置标准输入输出 - 交还终端执行逻辑
	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	// 监听系统信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 启动子进程
	if err := execCmd.Start(); err != nil {
		fmt.Printf("❌ 启动kubectl exec失败: %v\n", err)
		return
	}

	// 等待子进程退出或收到信号
	done := make(chan error, 1)
	go func() {
		done <- execCmd.Wait()
	}()

	select {
	case sig := <-sigChan:
		fmt.Printf("收到信号 %v，终止进程...\n", sig)
		cancel()                           // 通知子进程退出
		time.Sleep(500 * time.Millisecond) // 给子进程一点时间优雅退出
		execCmd.Process.Kill()             // 强制终止子进程
	case err := <-done:
		if err != nil {
			fmt.Printf("kubectl exec 异常退出: %v\n", err)
		}
	}
}
