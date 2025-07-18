package cmd

import (
    "fmt"
    "os/exec"
    "strings"
    "time"
    "github.com/spf13/cobra"
)

func init() {
    var namespace string
    var timeout int
    
    k8sCmd := &cobra.Command{
        Use:   "k8s [deployment-name]",
        Short: "监控Kubernetes部署状态",
        Long:  `监控通过Jenkins部署到Kubernetes的应用状态`,
        Run: func(cmd *cobra.Command, args []string) {
            if len(args) < 1 {
                fmt.Println("请指定部署名称")
                return
            }
            monitorK8sDeployment(args[0], namespace, timeout)
        },
    }
    
    k8sCmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes命名空间")
    k8sCmd.Flags().IntVarP(&timeout, "timeout", "t", 300, "超时时间(秒)")
    rootCmd.AddCommand(k8sCmd)
}

func monitorK8sDeployment(deploymentName, namespace string, timeout int) {
    fmt.Printf("监控部署 %s 在命名空间 %s 中的状态...\n", deploymentName, namespace)
    
    // 检查部署状态
    cmd := exec.Command("kubectl", "rollout", "status", 
        fmt.Sprintf("deployment/%s", deploymentName),
        "-n", namespace,
        fmt.Sprintf("--timeout=%ds", timeout))
    
    output, err := cmd.CombinedOutput()
    if err != nil {
        fmt.Printf("部署状态检查失败: %v\n%s\n", err, output)
        return
    }
    
    fmt.Printf("部署状态: %s\n", output)
    
    // 获取Pod状态
    getPodStatus(deploymentName, namespace)
    
    // 持续监控
    monitorPods(deploymentName, namespace)
}

func getPodStatus(deploymentName, namespace string) {
    cmd := exec.Command("kubectl", "get", "pods", 
        "-l", fmt.Sprintf("app=%s", deploymentName),
        "-n", namespace,
        "-o", "wide")
    
    output, err := cmd.Output()
    if err != nil {
        fmt.Printf("获取Pod状态失败: %v\n", err)
        return
    }
    
    fmt.Printf("Pod状态:\n%s\n", output)
}

func monitorPods(deploymentName, namespace string) {
    fmt.Println("开始实时监控Pod状态 (按Ctrl+C退出)...")
    
    for {
        cmd := exec.Command("kubectl", "get", "pods",
            "-l", fmt.Sprintf("app=%s", deploymentName),
            "-n", namespace,
            "--no-headers")
        
        output, err := cmd.Output()
        if err != nil {
            fmt.Printf("监控失败: %v\n", err)
            break
        }
        
        lines := strings.Split(strings.TrimSpace(string(output)), "\n")
        allReady := true
        
        for _, line := range lines {
            if line == "" {
                continue
            }
            fields := strings.Fields(line)
            if len(fields) >= 3 {
                status := fields[2]
                if status != "Running" {
                    allReady = false
                    fmt.Printf("Pod %s 状态: %s\n", fields[0], status)
                }
            }
        }
        
        if allReady && len(lines) > 0 {
            fmt.Printf("✅ 所有Pod运行正常 (%d个)\n", len(lines))
            break
        }
        
        time.Sleep(5 * time.Second)
    }
}