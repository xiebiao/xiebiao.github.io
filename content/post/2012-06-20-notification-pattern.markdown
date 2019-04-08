---
title: Notification Pattern理解
date:   2012-06-20 11:24:47
categories: ["Programming"]
tags: ["pattern"]
draft: false
---

## 场景
在MVC的架构中，我们想从Model层传递一些错误信息到View层，比如用户注册这个功能，用户填写了密码，邮箱，然后提交注册，
这时候我们后端需要对前端的数据做校验，同时将校验的结果返回给View层，这时候就能用到Notification模式.

## 思路
先定义Notification:

{% highlight java %}
public class Notification {
	private List _errors;

	public Notification() {
		_errors = new ArrayList();
	}
	public void clear() {
		_errors.clear();
	}
	public boolean hasError() {
		if (this._errors != null && this.errors().size() > 0) {
			return true;
		}
		return false;
	}
	public List errors() {
		return this._errors;
	}
	public class Error {
		private String _message;

		public Error(String messgae) {
			this._message = messgae;
		}
		public String getMessage() {
			return _message;
		}
		public void set_message(String _message) {
			this._message = _message;
		}
	}
}
{% endhighlight %}

定义IDomainValidator接口，如果是接口，notification就是实例共享的，在调用完errors方法后，需要clear，注意防止多线程问题，我这里用的抽象类 

{% highlight java %}
public abstract class IDomainValidator {
	/* 校验结果通知 */
	protected Notification notification;

	public IDomainValidator() {
		notification = new Notification();
	}
	abstract void validate();
}
{% endhighlight %}

领域模型：

{% highlight java %}
public class User extends IDomainValidator {
	private String name;
	private int age;
	public User(String name, int age) {
		this.name = name;
		this.age = age;
	}
	/**
	 * 校验领域模型
	 */
	public void validate() {
		if (age < 18) {
			notification.errors().add(notification.new Error("年龄小于18岁"));
		}
		if (name.equals("xiaog")) {
			notification.errors().add(notification.new Error("名字不能为:" + name));
		}
		// 当然你还可以对该领域做其他的校验
	}
	public String getName() {
		return name;
	}
	public void setName(String name) {
		this.name = name;
	}

	public int getAge() {
		return age;
	}
	public void setAge(int age) {
		this.age = age;
	}
}
{% endhighlight %}

测试: 

{% highlight java %}
public static void main(String[] args) {
      User user = new User("xiaog", 17);
      user.validate();
      if (user.notification.hasError()) {
        for (Notification.Error error : user.notification.errors()) {
		  System.out.println(error.getMessage());
        }
      }
     }
}
{% endhighlight %}

Notification模式的好处是： 

- 一次处理多条信息 
- 消耗更少的系统资源（也就是尽量在系统中少抛出Exception，少一些catch) 你还可以对这个模式进行扩展，将错误信息进一步封装。

## 参考
[Notification](http://martinfowler.com/eaaDev/Notification.html) 
