package cmd

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/chzyer/readline"
	"github.com/gocruncher/bar"
	"github.com/gocruncher/jenkins-job-cli/cmd/jj"
	"github.com/spf13/cobra"
	"github.com/ttacon/chalk"
)

var usageTamplate = `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

Available Commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`

var listenerStatus bool

type st struct {
	name  string
	id    int
	queue int
}

var curSt st
var barMutex sync.Mutex
var closeCh chan struct{}
var stdinListener *jjStdin

var verbose bool
var interactive bool

func init() {
	var runCmd = &cobra.Command{
		Use:     "run JOB",
		Aliases: []string{"r"},
		Short:   "Run the specified jenkins job",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				fmt.Println("请指定要运行的 Jenkins 任务名称")
				return
			}

			// 获取匹配的任务列表
			env := jj.Init(ENV)
			jobs := findMatchingJobs(env, args[0])

			// 若首次匹配不到，强制刷新 Jenkins 视图缓存后重试
			if len(jobs) == 0 {
				jj.RefreshBundle(env)
				jobs = findMatchingJobs(env, args[0])
			}

			if len(jobs) == 0 {
				fmt.Printf("未找到匹配的任务: %s\n", args[0])
				return
			}

			// 如果完全匹配某个任务名称，直接运行该任务
			for _, job := range jobs {
				if job == args[0] {
					runJob(job)
					return
				}
			}

			// 如果只有一个匹配项，直接运行
			if len(jobs) == 1 {
				runJob(jobs[0])
				return
			}

			// 多个匹配项，让用户选择
			fmt.Printf("\n找到 %d 个匹配的任务:\n", len(jobs))
			for i, job := range jobs {
				fmt.Printf("%d. %s\n", i+1, job)
			}

			rl, err := readline.New("请选择要运行的任务编号: ")
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

			runJob(jobs[index-1])
		},
		Args:         cobra.MaximumNArgs(1),
		PreRunE:      runPreRunE,
		SilenceUsage: false,
	}
	inputArgs = arguments{args: make([]string, 0, 20)}
	runCmd.Flags().StringArrayVarP(&inputArgs.args, "arg", "a", []string{}, "input arguments of a job. Usage: -a key=val")
	runCmd.Flags().StringVarP(&ENV, "name", "n", "", "current Jenkins name")
	// 添加 verbose 参数
	runCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "显示详细的构建输出")
	runCmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "交互式输入任务参数")
	runCmd.SetUsageTemplate(usageTamplate)
	rootCmd.AddCommand(runCmd)
}

func runPreRunE(cmd *cobra.Command, args []string) error {
	err := inputArgs.validate()
	if err != nil {
		return err
	}
	return preRunE(cmd, args)
}

func askParams(params []jj.ParameterDefinitions) map[string]string {
	data := map[string]string{}
	for _, pd := range params {
		cline := ""
		defVal := pd.DefaultParameterValue.Value
		curChoices := pd.Choices
		for {
			rl, err := NewReadLine(chalk.Underline.TextStyle(pd.Name)+": ", pd.Choices)
			defer rl.Close()
			if err != nil {
				os.Exit(1)
			}
			if pd.Type == "ChoiceParameterDefinition" {
				defVal = ""
			}
			line, err := rl.ReadlineWithDefault(defVal)
			line = strings.TrimSpace(line)
			if err != nil { // io.EOF
				os.Exit(1)
			}
			if pd.Type == "ChoiceParameterDefinition" {

				for _, val := range pd.Choices {
					if line == val {
						cline = val
						break
					}
				}
				if cline == "" {
					curChoices = findBestChoices(line, pd.Choices)
					if len(curChoices) == 0 {
						curChoices = pd.Choices
					} else if len(curChoices) == 1 {
						defVal = curChoices[0]
					} else {
						defVal = line
					}
					for _, val := range curChoices {
						fmt.Printf("%s\t", val)
					}
					if len(curChoices) > 0 {
						fmt.Println()
					}

					continue
				}
			} else {
				cline = line
			}
			break
		}

		data[pd.Name] = cline

	}
	return data
}

func paramsWithDefaults(params []jj.ParameterDefinitions, args arguments) map[string]string {
	data := map[string]string{}
	for _, pd := range params {
		data[pd.Name] = pd.DefaultParameterValue.Value
		if val, err := args.get(pd.Name); err == nil {
			data[pd.Name] = val
		}
	}
	return data
}

func runJob(name string) {
	env := jj.Init(ENV)
	time.Sleep(time.Millisecond * 200)
	fmt.Printf("Job will be started in the %s environment\n", chalk.Underline.TextStyle(string(env.Name)))
	time.Sleep(time.Millisecond * 200)
	if env.Url[len(env.Url)-1:] != "/" {
		env.Url = env.Url + "/"
	}
	fmt.Println("Link: ", env.Url+"job/"+name)
	time.Sleep(time.Millisecond * 200)

	bar.InitTerminal()
	data := map[string]string{}
	err, jobInfo := jj.GetJobInfo(env, name)
	if err == jj.ErrNoJob {
		err = fmt.Errorf("job '%s' does not exist", name)
	}
	check(err)
	params := jobInfo.GetParameterDefinitions()
	if interactive {
		data = askParams(params)
	} else {
		data = paramsWithDefaults(params, inputArgs)
	}

	urlquery := url.Values{}
	for key, val := range data {
		urlquery.Add(key, val)
	}
	err, queueId := jj.Build(env, name, urlquery.Encode())
	check(err)

	keyCh := make(chan string)
	stdinListener = NewStdin()
	go listenKeys(keyCh)
	go listenInterrupt(env)
	queueId1, _ := strconv.Atoi(queueId)
	curSt.queue = queueId1
	curSt.name = name
	number := waitForExecutor(env, queueId1)
	curSt.id = number
	err = watchTheJob(env, name, number, keyCh)
	if err != nil {
		return
	}
	curSt = st{}
	for _, jChild := range jobInfo.DownstreamProjects {
		err = watchNext(env, name, jChild.Name, number, keyCh)
		if err != nil {
			return
		}
		curSt = st{}
	}
	fmt.Println(chalk.Green.Color("done"))
	return
}

func waitForExecutor(env jj.Env, queueId int) int {
	informed := false
	for {
		err, queueInfo := jj.GetQueueInfo(env, queueId)
		check(err)
		if !queueInfo.Blocked && queueInfo.Executable.URL != "" {
			return queueInfo.Executable.Number
		} else {
			if !informed {
				//clearer := strings.Repeat(" ", int(110)-1)
				fmt.Println("waiting for next available executor..  ")
				informed = true
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func barHandler(jobUrl string, keyCh chan string, chMsg chan string, finishCh chan struct {
	err    error
	result string
}, wg *sync.WaitGroup) {
	defer wg.Done()
	barMutex.Lock()
	fmt.Print("\033[F")
	br := bar.NewWithOpts(
		bar.WithDimensions(100, 20),
		bar.WithLines(1),
		bar.WithFormat(
			fmt.Sprintf(
				"%srunning...%s :percent :bar %s:eta%s",
				chalk.White,
				chalk.Reset,
				chalk.Green,
				chalk.Reset)))
	br.Tick()
	barMutex.Unlock()
	for {
		select {
		case stdin, _ := <-keyCh:
			if []byte(stdin)[0] == 10 {
				barMutex.Lock()
				br.SetLines(br.GetLines() + 1)
				barMutex.Unlock()
			}
		case msg := <-chMsg:

			if msg != "" {
				barMutex.Lock()
				br.Interrupt(msg)
				barMutex.Unlock()
			} else {
				barMutex.Lock()
				br.Tick()
				barMutex.Unlock()
			}

		case info := <-finishCh:
			if info.err != nil && br.GetLines() < 5 {
				for br.GetLines() < 10 {
					barMutex.Lock()
					br.SetLines(br.GetLines() + 1)
					barMutex.Unlock()
				}
			}
			if info.err == nil {
				fmt.Printf("\r%s", strings.Repeat(" ", int(50)-1))
				fmt.Print("\033[F")
			}

			barMutex.Lock()
			br.SetFormat(fmt.Sprintf(jobUrl + ": " + info.result))
			br.Done()
			barMutex.Unlock()
			if info.err != nil {
				fmt.Println(chalk.Red.Color("failed"))
			}
			return
		case <-closeCh:
			return
		}
	}
}

// 在watchTheJob函数中添加部署后检查
func watchTheJob(env jj.Env, name string, number int, keyCh chan string) error {
	jobUrl := env.Url + "/job/" + name + "/" + strconv.Itoa(number) + "/console"
	lastBuild, _ := jj.GetLastSuccessfulBuildInfo(env, name)
	listenerStatus = true
	defer func() {
		listenerStatus = false
	}()
	ticks := 1
	cursor := "0"
	stime := getTime()
	chMsg := make(chan string)
	closeCh = make(chan struct{})
	finishCh := make(chan struct {
		err    error
		result string
	})
	needWatchDeployStatus := true
	var wg sync.WaitGroup
	wg.Add(1)
	go barHandler(jobUrl, keyCh, chMsg, finishCh, &wg)
	defer close(closeCh)
	defer wg.Wait()

	// 添加总体超时机制
	totalTimeout := 12 * time.Minute // 12分钟总超时
	timeoutTimer := time.NewTimer(totalTimeout)
	defer timeoutTimer.Stop()

	go func() {
		for {
			select {
			case <-time.After(time.Millisecond * 100):
				ctime := getTime()
				dtime := ctime - stime
				newTicks := int(float64(dtime) / float64(lastBuild.Duration) * 100)
				for ticks < newTicks && ticks < 99 {
					chMsg <- ""
					ticks++
				}
			case <-closeCh:
				return
			case <-timeoutTimer.C:
				fmt.Printf("\n⏰ Job 监控超时 (%.0f分钟)，自动退出\n", totalTimeout.Minutes())
				err := errors.New("timeout")
				finishCh <- struct {
					err    error
					result string
				}{err, "TIMEOUT"}
				return
			}
		}
	}()

	handle := func(cursor string, sleepTime int) string {
		output, nextCursor, err := jj.Console(env, name, number, cursor)
		if err != nil || cursor == nextCursor {
			return cursor
		}
		output = stripHTMLTags(output)
		lines := strings.Split(output, "\n")
		count := len(lines)
		if count > 50 {
			count = 50
		}

		// 根据 verbose 参数决定显示行数
		displayLines := make([]string, 0)
		seenLines := make(map[string]bool)

		for i := count; i >= 1; i-- {
			rline := []rune(string(lines[len(lines)-i]))
			if err != nil {
				break
			}

			j := 0
			size := 100
			for {
				var fline string
				s := j * size
				e := (j + 1) * size
				if len(rline) > e {
					fline = string(rline[s:e])
				} else {
					fline = string(rline[s:len(rline)])
				}

				trimmedLine := strings.TrimSpace(fline)
				// 如果 trimmedLine 中包含 front， front-boohee 或者 yarn 或者 npm 或者 pnpm 则不需要检查部署状态
				if needWatchDeployStatus {
					if strings.Contains(trimmedLine, "front-boohee") || strings.Contains(trimmedLine, "yarn") || strings.Contains(trimmedLine, "front/asset/") || strings.Contains(trimmedLine, "front/chunkScript") || strings.Contains(trimmedLine, "Webpack") {
						needWatchDeployStatus = false
					}
				}

				if len(trimmedLine) > 0 && !seenLines[trimmedLine] {
					seenLines[trimmedLine] = true
					displayLines = append(displayLines, fline)
				}

				j++
				if len(rline) <= e || len(rline) > 10*size {
					break
				}
			}

			// 根据 verbose 参数决定显示行数
			if verbose {
				// verbose 模式下每积累3行显示一次
				if len(displayLines) >= 3 || i == 1 {
					if len(displayLines) > 0 {
						chMsg <- strings.Join(displayLines, "\n")
						time.Sleep(time.Duration(sleepTime) * time.Millisecond)
						displayLines = make([]string, 0)
					}
				}
			} else {
				// 非 verbose 模式下每行立即显示
				if len(displayLines) > 0 {
					chMsg <- displayLines[len(displayLines)-1]
					time.Sleep(time.Duration(sleepTime) * time.Millisecond)
					displayLines = make([]string, 0)
				}
			}
		}
		return nextCursor
	}

	// 添加主循环超时检查
	mainLoopTimeout := 10 * time.Minute // 主循环10分钟超时
	mainLoopStart := time.Now()

	for {
		// 检查主循环是否超时
		if time.Since(mainLoopStart) > mainLoopTimeout {
			fmt.Printf("\n⏰ Job 主循环超时 (%.0f分钟)，自动退出\n", mainLoopTimeout.Minutes())
			err := errors.New("main loop timeout")
			finishCh <- struct {
				err    error
				result string
			}{err, "TIMEOUT"}
			return err
		}

		curBuild, err := jj.GetBuildInfo(env, name, number)
		if err != nil {
			if getTime()-stime > int64(60*time.Millisecond) {
				err := errors.New("failed")
				finishCh <- struct {
					err    error
					result string
				}{err, err.Error()}
				return err
			}
		} else {
			if !curBuild.Building {
				if curBuild.Result == "SUCCESS" {
					k := 0
					for {
						k++
						nc := handle(cursor, 1)
						if k > 5 {
							fmt.Println()
							fmt.Println("nc", nc, cursor)
							fmt.Println()
						}
						if nc == cursor {
							break
						}
						cursor = nc
					}

					finishCh <- struct {
						err    error
						result string
					}{nil, curBuild.Result}

					if needWatchDeployStatus {
						// 任务成功完成后检查K8s部署状态，添加超时控制
						fmt.Println("\n🔍 检查Kubernetes部署状态...")
						checkK8sDeployment(name)
					}
					return nil
				} else {
					err := errors.New("failed")
					finishCh <- struct {
						err    error
						result string
					}{err, curBuild.Result}
					return err
				}
			}
		}
		ncursor := handle(cursor, 100)
		if ncursor != cursor {
			cursor = ncursor
			//dotick()
			ctime := getTime()
			dtime := ctime - stime
			if dtime < 10 {
				time.Sleep(time.Duration(10-dtime) * time.Millisecond)
			}
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func watchNext(env jj.Env, parentName string, childName string, parentJobID int, keyCh chan string) error {
	for i := 0; ; i++ {
		bi, err := findDownstreamInBuilds(env, parentName, childName, parentJobID)
		if err != nil {
			queueId, err := findDownstreamInQueue(env, parentName, childName, parentJobID)
			curSt.queue = queueId
			curSt.name = childName
			if err != nil {
				time.Sleep(250 * time.Millisecond)
				continue
			}
			number := waitForExecutor(env, queueId)
			curSt.id = number
			return watchTheJob(env, childName, number, keyCh)
		} else {
			id, _ := strconv.Atoi(bi.Id)
			curSt.name = childName
			curSt.id = id
			return watchTheJob(env, childName, id, keyCh)
		}
	}
}

func findDownstreamInBuilds(env jj.Env, parentName string, childName string, parent int) (*jj.BuildInfo, error) {
	err, jobInfo := jj.GetJobInfo(env, childName)
	check(err)
	number := jobInfo.LastBuild.Number
	for i := 5; i >= 0; i-- {
		bi, err := jj.GetBuildInfo(env, childName, number-i)
		if err != nil {
			continue
		}
		for _, a := range bi.Actions {
			for _, c := range a.Causes {
				if c.UpstreamBuild == parent && c.UpstreamProject == parentName {
					return bi, nil
				}
			}
		}
	}
	return &jj.BuildInfo{}, errors.New("not found")
}

func findDownstreamInQueue(env jj.Env, parentName string, childName string, parentJobID int) (int, error) {
	queues := jj.GetQueues(env)
	for _, queue := range queues.Items {
		if queue.Task.Name == childName {
			for _, action := range queue.Actions {
				for _, cause := range action.Causes {
					if cause.UpstreamBuild == parentJobID && cause.UpstreamProject == parentName {
						return queue.ID, nil
					}
				}
			}
		}
	}
	return 0, errors.New("not found")
}

func listenKeys(out chan string) {
	stdinListener.NewListener()
	bt := make([]byte, 1)
	for {
		n, err := stdinListener.Read(bt)
		if err != nil || n == 0 {
			return
		}
		barMutex.Lock()
		if listenerStatus {
			out <- string(bt)
		}
		barMutex.Unlock()
	}

}

func listenInterrupt(env jj.Env) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for _ = range c {
			if curSt.name != "" {
				barMutex.Lock()

				defer barMutex.Unlock()
				stdinListener.NewListener()
				readline.Stdin = stdinListener
				rl, err := readline.New(fmt.Sprintf("There is active build: %s. Do you want to cancel it [Y/n]:", curSt.name))
				defer rl.Close()
				if err != nil {
					os.Exit(1)
				}
				line, err := rl.Readline()
				if err != nil { // io.EOF
					os.Exit(1)
				}
				if line == "Y" || line == "y" {

					if curSt.queue != 0 {
						fmt.Println("canceling queue...")
						jj.CancelQueue(env, curSt.queue)
					}
					if curSt.id != 0 {
						fmt.Println("canceling job...")
						status, err := jj.CancelJob(env, curSt.name, curSt.id)
						if err != nil {
							fmt.Printf("failed to cancel job, error %s", err)
							os.Exit(0)
						}
						if status != "ABORTED" {
							fmt.Printf("Job already has been executed, status: %s", status)
							os.Exit(0)
						}
						fmt.Println("Canceled")
						os.Exit(0)
					}
					if curSt.queue != 0 && curSt.id == 0 {
						err, jobInfo := jj.GetJobInfo(env, curSt.name)
						check(err)
						number := jobInfo.LastBuild.Number
						for i := 0; i < 3; i++ {
							bi, err := jj.GetBuildInfo(env, curSt.name, number-i)
							if err != nil {
								continue
							}
							if bi.QueueId == curSt.queue {
								if bi.Result != "ABORTED" {
									fmt.Printf("Job already has been executed, status: %s", bi.Result)
									os.Exit(0)
								} else {
									fmt.Println("Canceled!")
									os.Exit(0)
								}
							}
						}
						fmt.Println("Canceled!!!")
					}
				}
				os.Exit(0)
			}
		}
	}()
}

func check(err error) {
	if err != nil {
		fmt.Printf("\nError: %s\n", err.Error())
		os.Exit(1)
	}
}

// 查找匹配的任务
func findMatchingJobs(env jj.Env, pattern string) []string {
	matchedJobs := make(map[string]struct{}) // 使用 map 来去重
	bundle := jj.GetBundle(env)

	for _, view := range bundle.Views {
		for _, job := range view.Jobs {
			if strings.Contains(strings.ToLower(job.Name), strings.ToLower(pattern)) {
				matchedJobs[job.Name] = struct{}{} // 使用 map 自动去重
			}
		}
	}

	// 转换回切片
	result := make([]string, 0, len(matchedJobs))
	for jobName := range matchedJobs {
		result = append(result, jobName)
	}

	return result
}

// 添加一个辅助函数来去除 HTML 标签
func stripHTMLTags(text string) string {
	// 移除 HTML 标签
	re := regexp.MustCompile("<[^>]*>")
	text = re.ReplaceAllString(text, "")

	// 解码 HTML 实体
	text = html.UnescapeString(text)

	// 移除多余的空行
	lines := strings.Split(text, "\n")
	var result []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// 根据Jenkins任务名称提取Kubernetes部署名称
func extractDeploymentName(jobName string) string {
	// 移除常见的Jenkins任务前缀和后缀
	deploymentName := strings.ToLower(jobName)

	// 移除常见的前缀
	prefixes := []string{
		"deploy-", "deployment-", "k8s-", "kubernetes-",
		"build-", "ci-", "cd-", "pipeline-", "job-", "rc-",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(deploymentName, prefix) {
			deploymentName = strings.TrimPrefix(deploymentName, prefix)
			break
		}
	}

	// 移除常见的后缀
	suffixes := []string{
		"-deploy", "-deployment", "-k8s", "-kubernetes",
		"-build", "-ci", "-cd", "-pipeline",
		"-prod", "-production", "-staging", "-dev", "-development",
		"-test", "-testing", "-uat",
	}
	for _, suffix := range suffixes {
		if strings.HasSuffix(deploymentName, suffix) {
			deploymentName = strings.TrimSuffix(deploymentName, suffix)
			break
		}
	}

	// 替换不符合Kubernetes命名规范的字符
	// Kubernetes资源名称只能包含小写字母、数字和连字符
	deploymentName = regexp.MustCompile(`[^a-z0-9-]`).ReplaceAllString(deploymentName, "-")

	// 移除开头和结尾的连字符
	deploymentName = strings.Trim(deploymentName, "-")

	// 如果处理后的名称为空，使用原始名称的简化版本
	if deploymentName == "" {
		deploymentName = regexp.MustCompile(`[^a-zA-Z0-9-]`).ReplaceAllString(strings.ToLower(jobName), "-")
		deploymentName = strings.Trim(deploymentName, "-")
	}

	return deploymentName
}

func checkK8sDeployment(jobName string) {
	// 根据任务名称推断部署名称
	deploymentName := extractDeploymentName(jobName)

	fmt.Printf("🔍 检查部署: %s (从任务名 %s 推断)\n", deploymentName, jobName)
	if deploymentName == "" {
		fmt.Println("未推断到部署名称")
		return
	}

	fmt.Printf("⏱️  将监控100秒后自动退出 (按 Ctrl+C 可提前退出), 或者 存在状态获取失败 \n\n")

	// 使用 k8s.go 中的监控逻辑，添加超时和中断支持
	checkK8sDeploymentWithContext(deploymentName, "default", 100*time.Second)
}

// 为部署查找匹配的Pod
func findMatchingPodsForDeployment(deploymentName, namespace string) []string {
	// 获取所有Pod
	cmd := exec.Command("kubectl", "get", "pods", "-n", namespace, "--no-headers", "-o", "custom-columns=NAME:.metadata.name")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	allPods := strings.Split(strings.TrimSpace(string(output)), "\n")
	var matchedPods []string

	deploymentPattern := strings.ToLower(deploymentName)
	for _, pod := range allPods {
		if pod == "" {
			continue
		}
		// 以部署名称开头的 pod
		if strings.HasPrefix(strings.ToLower(pod), deploymentPattern) {
			matchedPods = append(matchedPods, pod)
		}
	}

	return matchedPods
}

// context 的写法是确实比手动管理 channel 要简洁，而且写起来，不容出错
func checkK8sDeploymentWithContext(deploymentName, namespace string, timeout time.Duration) {
	// 创建根上下文，并整合超时和中断信号
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // 确保所有派生上下文被取消

	// 设置信号处理（Ctrl+C/SIGTERM）
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-signalChan
		fmt.Println("\n👋 收到退出信号，停止检查...")
		cancel() // 触发上下文取消
	}()

	// 启动独立超时协程
	go func() {
		time.Sleep(timeout) // 阻塞等待超时
		fmt.Printf("\n⏰ 检查超时 (%.0f秒)，自动退出\n", timeout.Seconds())
		cancel() // 超时后主动取消
	}()

	// 启动Pod监控协程
	go func() {
		matchedPods := findMatchingPodsForDeployment(deploymentName, namespace)

		if len(matchedPods) == 0 {
			fmt.Printf("⚠️ 未找到匹配的Pod: %s\n", deploymentName)
			cancel()
			return
		} else {
			fmt.Printf("✅ 找到 %d 个匹配的Pod: %s\n", len(matchedPods), strings.Join(matchedPods, ", "))
			watchSpecificPodsWithContext(ctx, cancel, matchedPods, namespace)
		}
	}()

	// 主协程等待上下文结束
	<-ctx.Done()
}

// 使用Context监控Pod状态
func watchSpecificPodsWithContext(ctx context.Context, cancel context.CancelFunc, podNames []string, namespace string) {
	fmt.Printf("👀 监控特定Pod: %s (命名空间: %s)\n", strings.Join(podNames, ", "), namespace)
	currentFailures := 0
	failurePodName := ""

	for {
		select {
		case <-ctx.Done(): // 响应取消或超时
			return
		default:
			fmt.Printf("⏰ %s - 检查Pod状态...\n", time.Now().Format("15:04:05"))

			for _, podName := range podNames {
				cmd := exec.CommandContext(ctx, "kubectl", "get", "pod", podName, "-n", namespace, "--no-headers")
				output, err := cmd.Output()
				if err != nil {
					fmt.Printf("❌ %s: 获取状态失败 - %v\n", podName, err)
					currentFailures++
					failurePodName = podName
					continue
				}

				// 解析Pod状态
				line := strings.TrimSpace(string(output))
				if line == "" {
					fmt.Printf("⚠️  %s: Pod不存在\n", podName)
					continue
				}

				fields := strings.Fields(line)
				if len(fields) >= 3 {
					ready := fields[1]
					status := fields[2]
					age := fields[4]

					if status == "Running" && strings.Contains(ready, "/") {
						readyParts := strings.Split(ready, "/")
						if len(readyParts) == 2 && readyParts[0] == readyParts[1] {
							fmt.Printf("✅ %s: %s (%s)\n", podName, status, age)
						} else {
							fmt.Printf("⚠️  %s: %s (%s) - 未完全就绪\n", podName, status, age)
						}
					} else {
						fmt.Printf("❌ %s: %s (%s)\n", podName, status, ready)
					}
				}
			}

			if currentFailures > 1 {
				fmt.Printf("完成pod状态监控: 原先pod %s 已退出\n", failurePodName)
				cancel()
				return
			}

			fmt.Println(strings.Repeat("-", 50))
			time.Sleep(5 * time.Second)
		}
	}
}

// 带超时和中断支持的K8s部署检查 - 通过 channel  和 timeoutTimer 来实现超时
func checkK8sDeploymentWithTimeout(deploymentName, namespace string, timeout time.Duration) {
	// 设置信号处理，捕获 Ctrl+C
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// 创建超时定时器
	timeoutTimer := time.NewTimer(timeout)

	// 创建停止通道
	stopChan := make(chan bool)
	// 创建成功通道
	successChan := make(chan bool)

	// 确保在函数结束时关闭所有通道
	defer func() {
		signal.Stop(c) // 停止信号通知
		// 不在这里关闭通道，让发送方负责
		timeoutTimer.Stop()
	}()

	// 启动信号监听协程
	go func() {
		<-c
		fmt.Printf("\n\n👋 收到退出信号，停止检查...\n")

		select {
		case stopChan <- true: // 尝试发送停止信号
		default:
		}
	}()

	// 启动超时监听协程
	go func() {
		select {
		case <-timeoutTimer.C:
			fmt.Printf("\n\n⏰ 检查超时 (%.0f秒)，自动退出\n", timeout.Seconds())
			select {
			case stopChan <- true: // 尝试发送停止信号
			default:
			}
		case <-successChan:
			// 如果成功了，就不需要发送超时信号
			return
		}
	}()

	// 首先尝试找到匹配的Pod
	matchedPods := findMatchingPodsForDeployment(deploymentName, namespace)

	fmt.Printf("✅ 找到 %d 个匹配的Pod: %s\n", len(matchedPods), strings.Join(matchedPods, ", "))
	// 监控特定的Pod
	watchSpecificPodsWithTimeout(matchedPods, namespace, stopChan, successChan)
}

// 带停止通道的特定Pod监控
func watchSpecificPodsWithTimeout(podNames []string, namespace string, stopChan chan bool, successChan chan bool) {
	fmt.Printf("👀 监控特定Pod: %s (命名空间: %s)\n", strings.Join(podNames, ", "), namespace)
	currentFailures := 0 // 当前循环中的失败次数
	failurePodName := ""
	for {
		select {
		case <-stopChan:
			return
		default:
			fmt.Printf("⏰ %s - 检查Pod状态...\n", time.Now().Format("15:04:05"))

			for _, podName := range podNames {
				cmd := exec.Command("kubectl", "get", "pod", podName, "-n", namespace, "--no-headers")
				output, err := cmd.Output()
				if err != nil {
					fmt.Printf("❌ %s: 获取状态失败 - %v\n", podName, err)
					currentFailures++ // 当前循环中的失败次数
					failurePodName = podName
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
							fmt.Printf("✅ %s: %s (%s)\n", podName, status, ready)
						} else {
							fmt.Printf("⚠️  %s: %s (%s) - 未完全就绪\n", podName, status, ready)
						}
					} else {
						fmt.Printf("❌ %s: %s (%s)\n", podName, status, ready)
					}
				}
			}

			if currentFailures > 1 {
				fmt.Printf("完成pod状态监控: 原先pod %s 已出退  \n", failurePodName)
				select {
				case successChan <- true: // 尝试发送成功信号
				default:
				}
				select {
				case stopChan <- true: // 尝试发送停止信号
				default:
				}
				return
			} else {
				fmt.Printf(strings.Repeat("-", 50) + "\n")
				time.Sleep(5 * time.Second)
			}
		}
	}
}
