#  Easy way to run Jenkins job from the Command Line 
<meta name="google-site-verification" content="Wl2WZRolJ6omFNTQRguTy0GRQU41taSDq20n4Qgz05c" />

The utility starts a Jenkins build/job from the Command Line/Terminal.
An execution will be like this:

![terminal demo](assets/demo.gif)

## Install

Fetch the [latest release](https://github.com/xiajian2019/jenkins-job-cli/releases) for your platform:

#### Linux

```bash
sudo wget https://github.com/xiajian2019/jenkins-job-cli/releases/download/v1.1.3/jenkins-job-cli-1.1.3-linux-amd64 -O /usr/local/bin/jj
sudo chmod +x /usr/local/bin/jj
```

#### OS X bash

```bash
sudo curl -Lo /usr/local/bin/jj https://github.com/gocruncher/jenkins-job-cli/releases/download/v1.1.2/jenkins-job-cli-1.1.2-darwin-amd64
sudo chmod +x /usr/local/bin/jj
```

## Getting Started 

### Configure Access to Multiple Jenkins

```bash
jj set dev_jenkins --url "https://myjenkins.com" --login admin --token 11aa0926784999dab5  
```
where the token is available in your personal configuration page of the Jenkins. Go to the Jenkins Web Interface and click your name on the top right corner on every page, then click "Configure" to see your API token. 

In case, when Jenkins is available without authorization:
```bash
jj set dev_jenkins --url "https://myjenkins.com"  
```

or just run the following command in dialog execution mode:
```bash
jj set dev_jenkins
```


### Shell autocompletion

As a recommendation, you can enable shell autocompletion for convenient work. To do this, run following:
```bash
# for zsh completion:
echo 'source <(jj completion zsh)' >>~/.zshrc

# for bash completion:
echo 'source <(jj completion bash)' >>~/.bashrc
```
if this does not work for some reason, try following command that might help you to figure out what is wrong: 
```bash
jj completion check
```

### Examples

```bash
# Configure Access to the Jenkins
jj set dev-jenkins

# Start 'app-build' job in the current Jenkins
jj run app-build

# Start 'web-build' job in Jenkins named prod
jj run -n prod web-build

# makes a specific Jenkins name by default
jj use PROD  

jj get 
jj get -n prod

jj builds job-name
jj builds -v job-name 1
```

support check k8s deployment status after job finished， and check k8s deployment status by job name.


```bash
jj k8s pods                    # 查看所有Pod (简洁模式)
jj k8s pods myapp              # 模糊匹配包含myapp的Pod，支持选择
jj k8s pods -l service=web     # 使用自定义标签选择器
jj k8s pods myapp -w           # 实时监控Pod状态
jj k8s pods myapp --logs       # 查看Pod最近100行日志并实时追踪
jj k8s pods myapp --logs --no-follow  # 仅查看最近100行日志，不追踪
jj k8s pods myapp -d           # 显示详细信息
jj k8s pods myapp -e           # 进入容器中
jj k8s pods myapp -s           # 简洁模式 (仅显示基本状态)`
```

> Note: local kubectl need to access your k8s cluster. 
> You Can test by the following command line
> kubectl get pods


## Futures
- cancellation job (Ctrl+C key)
- resize of the output (just press enter key)
- output of child jobs   

## Useful packages
- [cobra](https://github.com/spf13/cobra) - library for creating powerful modern CLI
- [chalk](https://github.com/chalk/chalk) – Terminal string styling done right
- [bar](https://github.com/superhawk610/bar) - Flexible ascii progress bar.

## Todos
- add authorization by login/pass and through the RSA key
- support of a terminal window resizing

## Similar projects
* [jcli](https://github.com/jenkins-zh/jenkins-cli/) was written by Golang which can manage multiple Jenkins

## License
`jenkins-job-cli` is open-sourced software licensed under the [MIT](LICENSE) license.
