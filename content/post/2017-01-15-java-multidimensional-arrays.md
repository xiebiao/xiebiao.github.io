---
layout: post
title:  Java多维数组
date:   2017-01-15 20:24:47
categories: ["Programming"]
tag: java
---

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
恕我愚钝，尽管搞了这么多年java，还是第一次遇到这样的定义(因为工作中很少遇到这样的使用场景)。
先看看[Java language specification](http://docs.oracle.com/javase/specs/jls/se7/html/jls-10.html) 中对数组变量声明的定义.

```
type arrayName[][[]...];
```
或

```
type[][[]...] arrayName;
```
其实Java中的数组可以用矩阵来表示,比如 *int[][] a[]* 则可以看成:

![矩阵](http://posts.xiebiao.com/images/2017-01-15-java-multidimensional-arrays/matrix.png "矩阵")

*int[][] a[]*  等同于 *int[][][] a* ，你可以叫为三维数组，
所以 *int[][] a[][][]* 也同于 *int[][][][][] a*，叫为五维数组。

其实从[Java language specification](http://docs.oracle.com/javase/specs/jls/se7/html/jls-10.html)中可以看出
*int[][]* 可以理解为 *type*，尽管定义中指出不推荐 *mixed notation* 方式来声明多维数组,  但不影响这种表达式合法。
