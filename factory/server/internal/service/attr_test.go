// 路径快照小工具：按节点身份查路径里有没有、叫什么。
package service_test

import (
	factory "wmesh/factory/internal/service"
)

// 看路径里有没有这个节点，没有说明快照漏记。
func pathHas(path []factory.PathNode, id interface{ String() string }) bool { // 要能把编号打成文本
	want := id.String()
	for _, n := range path {
		// 节点对上就返回，对不上则继续看路径剩下的。
		if n.ID.String() == want {
			return true
		}
	}
	return false
}

// 取出路径上的名称，对不上说明改名没进快照。
func pathName(path []factory.PathNode, id interface{ String() string }) string { // 要能把编号打成文本
	want := id.String()
	for _, n := range path {
		// 节点对上就返回，对不上则继续看路径剩下的。
		if n.ID.String() == want {
			return n.Name
		}
	}
	return ""
}
