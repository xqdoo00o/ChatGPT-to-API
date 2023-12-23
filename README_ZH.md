# ChatGPT-to-API
从ChatGPT网站模拟使用API

**模拟API地址: http://127.0.0.1:8080/v1/chat/completions.**

## 使用
    
### 设置

配置账户邮箱和密码，自动生成和更新Access tokens 和 PUID（仅PLUS账户）（使用[OpenAIAuth](https://github.com/xqdoo00o/OpenAIAuth/)）

`accounts.txt` - 存放OpenAI账号邮箱和密码的文件

格式:
```
邮箱:密码
邮箱:密码
...
```

所有登录后的Access tokens和PUID会存放在`access_tokens.json`

每7天自动更新Access tokens和PUID

注意！ 请使用未封锁的ip登录账号，请先打开浏览器登录`https://chat.openai.com/`以检查ip是否可用

### HAR文件池

  当前登录账号，使用GPT-4模型以及大部分GPT-3.5模型，均需要配置HAR文件（.har后缀名的文件）以完成captcha验证。

  1. 使用基于chromium的浏览器（Chrome，Edge）打开浏览器开发者工具（F12），并切换到网络标签页，勾选**保留日志**选项。

  2. 登录`https://chat.openai.com/`，新建聊天并选择GPT-4模型，随意输入下文字，切换到GPT-3.5模型，随意输入下文字。

  3. 点击网络标签页下的导出HAR按钮，导出文件`chat.openai.com.har`，放置到本程序同级的`harPool`文件夹里。

### API 密钥（可选）

如OpenAI的官方API一样，可给模拟的API添加API密钥认证

`api_keys.txt` - 存放API密钥的文件

格式:
```
sk-123456
88888888
...
```

## 开始
```  
git clone https://github.com/xqdoo00o/ChatGPT-to-API
cd ChatGPT-to-API
go build
./freechatgpt
```

### 环境变量
  - `SERVER_HOST` - 默认127.0.0.1
  - `SERVER_PORT` - 默认8080
  - `ENABLE_HISTORY` - 默认false，不允许网页端历史记录

### 可选文件配置
  - `proxies.txt` - 存放代理地址的文件

    ```
    http://127.0.0.1:8888
    socks5://127.0.0.1:9999
    ...
    ```
  - `access_tokens.json` - 一个存放Access tokens 和PUID JSON数组的文件 (可使用 PATCH请求更新Access tokens [correct endpoint](https://github.com/xqdoo00o/ChatGPT-to-API/blob/master/docs/admin.md))
    ```
    {"邮箱1":{token:"access_token1", puid:"puid1"}, "邮箱2":{token:"access_token2", puid:"puid2"}...}
    ```
  - `cookies.json` - 一个存放登录cookies的文件，如果OpenAI账户为谷歌等第三方登录（第一方账号也同样适用），可在`accounts.txt`添加第三方账户和任意密码，修改此文件如下即可正常登录
    ```
    {
        "第三方账户名": [
            {
                "Name": "__Secure-next-auth.session-token",
                "Value": "网页登录第三方账户后，cookies中的__Secure-next-auth.session-token值",
                "Path": "/",
                "Domain": "",
                "Expires": "0001-01-01T00:00:00Z",
                "MaxAge": 0,
                "Secure": true,
                "HttpOnly": true,
                "SameSite": 2,
                "Unparsed": null
            }
        ]
    }
    ```

## 用户管理文档
https://github.com/xqdoo00o/ChatGPT-to-API/blob/master/docs/admin.md

## API使用说明
https://platform.openai.com/docs/api-reference/chat
