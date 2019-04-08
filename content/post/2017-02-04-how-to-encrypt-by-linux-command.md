---
layout: post
title:  如何利用Linux命令简单加密文本
date:   2017-02-04 18:24:47
categories: ["Programming"]
tag: security
---

在web上无论企业还是个人网站，总会发布一些邮箱的联系方式，由于邮箱地址置于html页面中，很容易被
爬虫获取，导致邮箱被垃圾邮件挤满。

所以，如果你确定要暴露你的邮箱到互联网上，那么你至少应该使用一种加密方式。

下面介绍两种简单加密方法。

## 使用xxd,bc,dc命令加密解密

第一步(将文本转换为16进制):

> `$ echo "xxxxx@gmail.com"|xxd -ps -u` 

> `$ 787878787840676D61696C2E636F6D0A`

第二步(将16进制转换为10进制):

> `$ echo "ibase=16; 787878787840676D61696C2E636F6D0A"|bc`

> `$ 160132878550962084828530736251255352586`


解密:

> `$ dc -e 160132878550962084828530736251255352586P`

## 使用base64编码

第一步: 

> `$ echo "xxxxx@gmail.com"|base64 -i`

> `$ eHh4eHhAZ21haWwuY29tCg==`

第二步:

> `$ echo "eHh4eHhAZ21haWwuY29tCg=="|base64 -d`

> `$ xxxxx@gmail.com`

## 总结

上面的方法只是防止人和机器直接识别出文本，并算不上真正意义上的加密。
