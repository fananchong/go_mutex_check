## 使用

类似以下脚本：

```shell
TOOLS_DIR=${PWD}
WORKSPACE=/your_workspace_path
go build
pushd ${WORKSPACE}
${TOOLS_DIR}/mutex_check --path=.
popd
```


## 变量类型

- [x] 全局变量
- [x] 结构体字段
- [ ] 局部变量（天然安全）
- [ ] 形参（勾选的检查做好后，安全）
- [x] 函数返回值
- [x] 结构体方法返回值


勾选的变量类型都要处理

## 声明约定

1. sync.Mutex sync.RWMutex 变量声明**加注释，标注要锁操作的变量或字段**

如以下例子：

```go
var m sync.Mutex // a,b,c
var a int
var b = map[int]int{}
var c string

type A1 struct {
	M sync.RWMutex // A
	A int
}
```


## 全局变量 - 检查步骤

1. 获取需要加锁的全局变量 A
2. 获取哪些函数 B ，直接使用了全局变量 A
3. 剔除 B 中有加锁的函数，得 C
4. 查看调用关系，逆向检查上级调用是否加锁
   1. 顶级函数也未加锁，报错
   2. 调用链超出本包路径，报错


## 结构体字段 - 检查步骤

1. 获取含有 mutex 字段的结构体、以及结构体内 mutex 对应的字段 A
2. 获取哪些函数 B ，直接使用了相关字段
3. 剔除 B 中有加锁的函数，得 C
4. 查看调用关系，逆向检查上级调用是否加锁
   1. 顶级结构体方法也未加锁，报错
   2. 调用链超出顶级结构体方法，报错

## 函数返回值 - 检查步骤（TODO）

1. 有 return 要加锁的全局变量，直接报错


## 结构体方法返回值 - 检查步骤（TODO）

1. 有 return 要加锁的结构体字段，直接报错
