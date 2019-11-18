---
layout: post
title: Captain's log, stardate [-26]0430.00
date: 2019-11-17 12:00 -0800
tags: [Captain's log]
---

### Mon, 18 Nov 2019
`vstest.console.exe` is a great tool to run tests from the command line. I have
been using this to automate some of my workflows. The command line arguments
are a little cumbersome. One option that I found useful was `/TestCaseFilter1`.
This option can be used to run specific tests by substring search and filter.
For example,

```
vstest.console.exe bin\Release\MyTests.dll /TestCaseFilter:"(FullyQualifiedName~ProdTest)|(FullyQualifiedName~DevTest.SALT)"
```
{: .code-wrap}

More information about the options and usage can be found at [1] and [2].

[1]: https://docs.microsoft.com/en-us/visualstudio/test/vstest-console-options?view=vs-2019
[2]: https://blogs.msdn.microsoft.com/vikramagrawal/2012/07/23/running-selective-unit-tests-in-vs-2012-rc-using-testcasefilter/
