package cmd

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"github.com/gocruncher/jenkins-job-cli/cmd/jj"
	"github.com/spf13/cobra"
)

func init() {
	var verbose bool
	buildsCmd := &cobra.Command{
		Use:   "builds [job_name] [build_number]",
		Short: "查看指定 Jenkins 任务的构建明细",
		Long: `查看指定 Jenkins 任务的构建明细列表。
如果不指定构建号，则显示最近的构建列表。
如果指定构建号，则显示该构建的详细信息。`,
		Run: func(cmd *cobra.Command, args []string) {
			showBuilds(args, verbose)
		},
	}

	buildsCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "显示构建的控制台输出")
	rootCmd.AddCommand(buildsCmd)
}

func showBuilds(args []string, verbose bool) {
	if len(args) == 0 {
		fmt.Println("请指定要查看的 Jenkins 任务名称")
		return
	}

	// Fix: Change the order of return values
	env := jj.Init(ENV)

	// 获取匹配的任务列表
	jobs := findMatchingJobs(env, args[0])

	if len(jobs) == 0 {
		fmt.Printf("未找到匹配的任务: %s\n", args[0])
		return
	}

	// 如果完全匹配某个任务名称，直接显示该任务
	for _, job := range jobs {
		if job == args[0] {
			showJobBuilds(env, job, args, verbose)
			return
		}
	}

	// 如果只有一个匹配项，直接显示
	if len(jobs) == 1 {
		showJobBuilds(env, jobs[0], args, verbose)
		return
	}

	// 多个匹配项，让用户选择
	fmt.Printf("\n找到 %d 个匹配的任务:\n", len(jobs))
	for i, job := range jobs {
		fmt.Printf("%d. %s\n", i+1, job)
	}

	rl, err := readline.New("\n请选择要查看的任务编号: ")
	if err != nil {
		fmt.Printf("读取输入失败: %v\n", err)
		return
	}
	defer rl.Close()

	line, err := rl.Readline()
	if err != nil {
		fmt.Printf("读取输入失败: %v\n", err)
		return
	}

	index, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || index < 1 || index > len(jobs) {
		fmt.Println("无效的选择")
		return
	}

	showJobBuilds(env, jobs[index-1], args, verbose)
}

// 新增函数：处理单个任务的构建信息显示
func showJobBuilds(env jj.Env, jobName string, args []string, verbose bool) {
	err, jobInfo := jj.GetJobInfo(env, jobName)
	if err != nil {
		fmt.Printf("获取任务信息失败: %v\n", err)
		return
	}

	if len(args) > 1 {
		// 显示指定构建号的详细信息
		buildNum, err := strconv.Atoi(args[1])
		if err != nil {
			fmt.Printf("无效的构建号: %s\n", args[1])
			return
		}
		showBuildDetail(env, jobName, buildNum, verbose)
	} else {
		// 显示最近的构建列表
		showBuildList(env, jobName, jobInfo)
	}
}

func showBuildList(env jj.Env, jobName string, jobInfo *jj.JobInfo) {
	fmt.Printf("\n任务名称: %s\n", jobName)
	fmt.Printf("最新构建号: #%d\n", jobInfo.NextBuildNumber-1)
	fmt.Printf("最后完成的构建: #%d\n", jobInfo.LastCompletedBuild.Number)
	fmt.Printf("是否在队列中: %v\n\n", jobInfo.InQueue)

	// 获取最近的构建列表
	// Fix: Change jj.Req to jj.req
	code, rsp, _, err := jj.Req(env, "GET", fmt.Sprintf("job/%s/api/json?tree=builds[number,result,timestamp,duration,building]", jobName), []byte{})
	if err != nil || code != 200 {
		fmt.Printf("获取构建列表失败: %v\n", err)
		return
	}

	var builds struct {
		Builds []struct {
			Number    int    `json:"number"`
			Result    string `json:"result"`
			Timestamp int64  `json:"timestamp"`
			Duration  int    `json:"duration"`
			Building  bool   `json:"building"`
		} `json:"builds"`
	}

	if err := json.Unmarshal(rsp, &builds); err != nil {
		fmt.Printf("解析构建列表失败: %v\n", err)
		return
	}

	fmt.Printf("最近构建列表:\n")
	fmt.Printf("构建号\t状态\t\t耗时\t\t开始时间\t\t控制台输出\n")
	fmt.Printf("--------------------------------------------------------------------------------\n")

	for _, build := range builds.Builds {
		status := build.Result
		if build.Building {
			status = "构建中"
		} else if status == "" {
			status = "未知"
		}

		startTime := time.Unix(build.Timestamp/1000, 0).Format("2006-01-02 15:04:05")
		duration := fmt.Sprintf("%dm%ds", build.Duration/60000, (build.Duration%60000)/1000)
		consoleUrl := fmt.Sprintf("%s/job/%s/%d/console", env.Url, jobName, build.Number)

		fmt.Printf("#%d\t%s\t\t%s\t%s\t%s\n",
			build.Number,
			status,
			duration,
			startTime,
			consoleUrl)
	}
}

func showBuildDetail(env jj.Env, jobName string, buildNum int, verbose bool) {
	// Fix: Change jj.Req to jj.req
	code, rsp, _, err := jj.Req(env, "GET", fmt.Sprintf("job/%s/%d/api/json", jobName, buildNum), []byte{})
	if err != nil || code != 200 {
		fmt.Printf("获取构建详情失败: %v\n", err)
		return
	}

	var buildInfo jj.BuildInfo
	if err := json.Unmarshal(rsp, &buildInfo); err != nil {
		fmt.Printf("解析构建详情失败: %v\n", err)
		return
	}

	fmt.Printf("\n构建详情 #%d:\n", buildNum)
	fmt.Printf("----------------------------------------\n")
	fmt.Printf("状态: %s\n", buildInfo.Result)
	fmt.Printf("构建中: %v\n", buildInfo.Building)
	fmt.Printf("持续时间: %ds\n", buildInfo.Duration/1000)
	fmt.Printf("控制台输出: %s/job/%s/%d/console\n", env.Url, jobName, buildNum)

	if verbose {
		// 获取并显示控制台输出
		code, rsp, _, err = jj.Req(env, "GET", fmt.Sprintf("job/%s/%d/consoleText", jobName, buildNum), []byte{})
		if err != nil || code != 200 {
			fmt.Printf("获取控制台输出失败: %v\n", err)
			return
		}

		fmt.Printf("\n控制台输出:\n")
		fmt.Printf("----------------------------------------\n")
		fmt.Printf("%s\n", string(rsp))
	}

	if len(buildInfo.Actions) > 0 {
		fmt.Println("\n构建参数:")
		for _, action := range buildInfo.Actions {
			for _, param := range action.Parameters {
				fmt.Printf("%s: %s\n", param.Name, param.Value)
			}
		}
	}
}
