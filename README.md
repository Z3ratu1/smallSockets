# smallSockets
一个简单的内网socks代理，实现了内网client反连server后的socks5代理  

代码量不大所以体积也还行，编译出来5M，压缩体积可以到3M。  
学习用项目，大概能比较简单的看懂内网代理的运行原理吧。。。但是同步异步不是很会，代码不一定很好看  
主要目的为自己写一遍理解一下这些内网打洞都是怎么打起来的，顺便理解了一下go的同步异步ctx channel等操作。开发水平++（大概）   
go的同步异步协程真的太顶级了8
顺便试了下最近看到的几个go的顶级第三方库之类的。

不保证稳定性速度并发等性能 :(

cobra写命令行真的牛逼
## usage
```shell
$ ./smallSocks
A simple NAT traversal tool that provide socks5 proxy

Usage:
  smallSocks [command]

Available Commands:
  client      connect to server to provide socks5 service
  help        Help about any command
  server      start server at control port

Flags:
  -a, --auth string    auth string between client and server(optional)
  -h, --help           help for smallSocks
  -l, --level string   log lever(debug/info/error)(optional)

Use "smallSocks [command] --help" for more information about a command.
```
