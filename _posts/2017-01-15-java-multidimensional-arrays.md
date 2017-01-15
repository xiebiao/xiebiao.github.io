---
layout: post
title:  "Java多维数组"
date:   2017-01-15 20:24:47
categories: java
---

http://docs.oracle.com/javase/specs/jls/se7/html/jls-10.html

## 一切看起来挺不错的

说多维数组之前，说说简单的数组定义:

{% highlight java %}
int[] a;
{% endhighlight %}

或者

{% highlight java %}
int a[];
{% endhighlight %}

当然也可以一次性定义多个变量

{% highlight java %}
int a[], b[],c[];
{% endhighlight %}

再简单不过了。

继续看看下面的呢

{% highlight java %}
int[][] a[][][];
{% endhighlight %}

这是个几维数组?
恕我愚钝，尽管搞了这没多年java，还是第一次遇到这样的定义(因为工作中很少遇到这样的使用场景)。
先看看[Java language specification](http://docs.oracle.com/javase/specs/jls/se7/html/jls-10.html) 中的定义.

```
type arrayName[][[]...];
```
或

```
type[][[]...] arrayName;
```
所以int[][] a[][][] 的意思是a的类型是一个二维