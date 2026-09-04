// @ts-nocheck
import React from 'react';
import { dynamic } from 'dumi';

export default {
  'docs-demo': {
    component: function DumiDemo() {
  var _interopRequireDefault = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireDefault.js")["default"];

  var _react = _interopRequireDefault(require("react"));

  var _echartsForReact = _interopRequireDefault(require("echarts-for-react"));

  var Page = function Page() {
    var options = {
      grid: {
        top: 8,
        right: 8,
        bottom: 24,
        left: 36
      },
      xAxis: {
        type: 'category',
        data: ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']
      },
      yAxis: {
        type: 'value'
      },
      series: [{
        data: [820, 932, 901, 934, 1290, 1330, 1320],
        type: 'line',
        smooth: true
      }],
      tooltip: {
        trigger: 'axis'
      }
    };
    return /*#__PURE__*/_react["default"].createElement(_echartsForReact["default"], {
      option: options
    });
  };

  var _default;

  return _react["default"].createElement(Page);
},
    previewerProps: {"sources":{"_":{"tsx":"import React from 'react';\nimport ReactECharts from 'echarts-for-react';\n\nconst Page: React.FC = () => {\n  const options = {\n    grid: { top: 8, right: 8, bottom: 24, left: 36 },\n    xAxis: {\n      type: 'category',\n      data: ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'],\n    },\n    yAxis: {\n      type: 'value',\n    },\n    series: [\n      {\n        data: [820, 932, 901, 934, 1290, 1330, 1320],\n        type: 'line',\n        smooth: true,\n      },\n    ],\n    tooltip: {\n      trigger: 'axis',\n    },\n  };\n\n  return <ReactECharts option={options} />;\n};\n\nexport default Page;"}},"dependencies":{"react":{"version":"^15.0.0 || >=16.0.0"},"echarts-for-react":{"version":"3.0.6"},"echarts":{"version":"^3.0.0 || ^4.0.0 || ^5.0.0 || ^6.0.0"}},"identifier":"docs-demo"},
  },
  'api-demo': {
    component: function DumiDemo() {
  var _interopRequireDefault = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireDefault.js")["default"];

  var _interopRequireWildcard = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireWildcard.js")["default"];

  var _react = _interopRequireWildcard(require("react"));

  var _echartsForReact = _interopRequireDefault(require("echarts-for-react"));

  var Page = function Page() {
    var option = {
      title: {
        text: '漏斗图',
        subtext: '纯属虚构'
      },
      tooltip: {
        trigger: 'item',
        formatter: "{a} <br/>{b} : {c}%"
      },
      toolbox: {
        feature: {
          dataView: {
            readOnly: false
          },
          restore: {},
          saveAsImage: {}
        }
      },
      legend: {
        data: ['展现', '点击', '访问', '咨询', '订单']
      },
      series: [{
        name: '预期',
        type: 'funnel',
        left: '10%',
        width: '80%',
        label: {
          normal: {
            formatter: '{b}预期'
          },
          emphasis: {
            position: 'inside',
            formatter: '{b}预期: {c}%'
          }
        },
        labelLine: {
          normal: {
            show: false
          }
        },
        itemStyle: {
          normal: {
            opacity: 0.7
          }
        },
        data: [{
          value: 60,
          name: '访问'
        }, {
          value: 40,
          name: '咨询'
        }, {
          value: 20,
          name: '订单'
        }, {
          value: 80,
          name: '点击'
        }, {
          value: 100,
          name: '展现'
        }]
      }, {
        name: '实际',
        type: 'funnel',
        left: '10%',
        width: '80%',
        maxSize: '80%',
        label: {
          normal: {
            position: 'inside',
            formatter: '{c}%',
            textStyle: {
              color: '#fff'
            }
          },
          emphasis: {
            position: 'inside',
            formatter: '{b}实际: {c}%'
          }
        },
        itemStyle: {
          normal: {
            opacity: 0.5,
            borderColor: '#fff',
            borderWidth: 2
          }
        },
        data: [{
          value: 30,
          name: '访问'
        }, {
          value: 10,
          name: '咨询'
        }, {
          value: 5,
          name: '订单'
        }, {
          value: 50,
          name: '点击'
        }, {
          value: 80,
          name: '展现'
        }]
      }]
    };
    var instance = (0, _react.useRef)(null);

    function clickBtn() {
      var base64 = instance.current.getEchartsInstance().getDataURL();
      var img = new Image();
      img.src = base64;
      var newWin = window.open('', '_blank');
      newWin.document.write(img.outerHTML);
    }

    return /*#__PURE__*/_react["default"].createElement(_react["default"].Fragment, null, /*#__PURE__*/_react["default"].createElement(_echartsForReact["default"], {
      ref: instance,
      option: option,
      style: {
        height: 400
      }
    }), /*#__PURE__*/_react["default"].createElement("div", null, /*#__PURE__*/_react["default"].createElement("button", {
      onClick: clickBtn
    }, "click here to get the DataURL of chart.")));
  };

  var _default;

  return _react["default"].createElement(Page);
},
    previewerProps: {"sources":{"_":{"tsx":"import React, { useRef } from 'react';\nimport ReactECharts from 'echarts-for-react';\n\nconst Page: React.FC = () => {\n  const option = {\n    title: {\n      text: '漏斗图',\n      subtext: '纯属虚构'\n    },\n    tooltip: {\n      trigger: 'item',\n      formatter: \"{a} <br/>{b} : {c}%\"\n    },\n    toolbox: {\n      feature: {\n        dataView: {readOnly: false},\n        restore: {},\n        saveAsImage: {}\n      }\n    },\n    legend: {\n      data: ['展现','点击','访问','咨询','订单']\n    },\n    series: [\n      {\n        name: '预期',\n        type: 'funnel',\n        left: '10%',\n        width: '80%',\n        label: {\n          normal: {\n            formatter: '{b}预期'\n          },\n          emphasis: {\n            position:'inside',\n            formatter: '{b}预期: {c}%'\n          }\n        },\n        labelLine: {\n          normal: {\n            show: false\n          }\n        },\n        itemStyle: {\n          normal: {\n            opacity: 0.7\n          }\n        },\n        data: [\n          {value: 60, name: '访问'},\n          {value: 40, name: '咨询'},\n          {value: 20, name: '订单'},\n          {value: 80, name: '点击'},\n          {value: 100, name: '展现'}\n        ]\n      },\n      {\n        name: '实际',\n        type: 'funnel',\n        left: '10%',\n        width: '80%',\n        maxSize: '80%',\n        label: {\n          normal: {\n            position: 'inside',\n            formatter: '{c}%',\n            textStyle: {\n              color: '#fff'\n            }\n          },\n          emphasis: {\n            position:'inside',\n            formatter: '{b}实际: {c}%'\n          }\n        },\n        itemStyle: {\n          normal: {\n            opacity: 0.5,\n            borderColor: '#fff',\n            borderWidth: 2\n          }\n        },\n        data: [\n          {value: 30, name: '访问'},\n          {value: 10, name: '咨询'},\n          {value: 5, name: '订单'},\n          {value: 50, name: '点击'},\n          {value: 80, name: '展现'}\n        ]\n      }\n    ]\n  };\n\n  const instance = useRef(null);\n\n  function clickBtn() {\n    const base64 = instance.current.getEchartsInstance().getDataURL();\n\n    const img = new Image();\n    img.src = base64;\n    const newWin = window.open('', '_blank');\n    newWin.document.write(img.outerHTML);\n  }\n\n  return (\n    <>\n      <ReactECharts\n        ref={instance}\n        option={option}\n        style={{ height: 400 }}\n      />\n      <div>\n        <button onClick={clickBtn}>click here to get the DataURL of chart.</button>\n      </div>\n    </>\n  );\n};\n\nexport default Page;"}},"dependencies":{"react":{"version":"^15.0.0 || >=16.0.0"},"echarts-for-react":{"version":"3.0.6"},"echarts":{"version":"^3.0.0 || ^4.0.0 || ^5.0.0 || ^6.0.0"}},"identifier":"api-demo"},
  },
  'dynamic-demo': {
    component: function DumiDemo() {
  var _interopRequireWildcard = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireWildcard.js")["default"];

  var _interopRequireDefault = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireDefault.js")["default"];

  var _slicedToArray2 = _interopRequireDefault(require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/esm/slicedToArray.js"));

  var _react = _interopRequireWildcard(require("react"));

  var _echartsForReact = _interopRequireDefault(require("echarts-for-react"));

  var _lodash = _interopRequireDefault(require("lodash.clonedeep"));

  var Page = function Page() {
    var DEFAULT_OPTION = {
      title: {
        text: 'Hello Echarts-for-react.'
      },
      tooltip: {
        trigger: 'axis'
      },
      legend: {
        data: ['最新成交价', '预购队列']
      },
      toolbox: {
        show: true,
        feature: {
          dataView: {
            readOnly: false
          },
          restore: {},
          saveAsImage: {}
        }
      },
      grid: {
        top: 60,
        left: 30,
        right: 60,
        bottom: 90
      },
      dataZoom: {
        show: false,
        start: 0,
        end: 100
      },
      visualMap: {
        show: false,
        min: 0,
        max: 1000,
        color: ['#BE002F', '#F20C00', '#F00056', '#FF2D51', '#FF2121', '#FF4C00', '#FF7500', '#FF8936', '#FFA400', '#F0C239', '#FFF143', '#FAFF72', '#C9DD22', '#AFDD22', '#9ED900', '#00E500', '#0EB83A', '#0AA344', '#0C8918', '#057748', '#177CB0']
      },
      xAxis: [{
        type: 'category',
        boundaryGap: true,
        data: function () {
          var now = new Date();
          var res = [];
          var len = 50;

          while (len--) {
            res.unshift(now.toLocaleTimeString().replace(/^\D*/, ''));
            now = new Date(now - 2000);
          }

          return res;
        }()
      }, {
        type: 'category',
        boundaryGap: true,
        data: function () {
          var res = [];
          var len = 50;

          while (len--) {
            res.push(50 - len + 1);
          }

          return res;
        }()
      }],
      yAxis: [{
        type: 'value',
        scale: true,
        name: '价格',
        max: 20,
        min: 0,
        boundaryGap: [0.2, 0.2]
      }, {
        type: 'value',
        scale: true,
        name: '预购量',
        max: 1200,
        min: 0,
        boundaryGap: [0.2, 0.2]
      }],
      series: [{
        name: '预购队列',
        type: 'bar',
        xAxisIndex: 1,
        yAxisIndex: 1,
        itemStyle: {
          normal: {
            barBorderRadius: 4
          }
        },
        animationEasing: 'elasticOut',
        animationDelay: function animationDelay(idx) {
          return idx * 10;
        },
        animationDelayUpdate: function animationDelayUpdate(idx) {
          return idx * 10;
        },
        data: function () {
          var res = [];
          var len = 50;

          while (len--) {
            res.push(Math.round(Math.random() * 1000));
          }

          return res;
        }()
      }, {
        name: '最新成交价',
        type: 'line',
        data: function () {
          var res = [];
          var len = 0;

          while (len < 50) {
            res.push((Math.random() * 10 + 5).toFixed(1) - 0);
            len++;
          }

          return res;
        }()
      }]
    };
    var count;

    var _useState = (0, _react.useState)(DEFAULT_OPTION),
        _useState2 = (0, _slicedToArray2["default"])(_useState, 2),
        option = _useState2[0],
        setOption = _useState2[1];

    function fetchNewData() {
      var axisData = new Date().toLocaleTimeString().replace(/^\D*/, '');
      var newOption = (0, _lodash["default"])(option); // immutable

      newOption.title.text = 'Hello Echarts-for-react.' + new Date().getSeconds();
      var data0 = newOption.series[0].data;
      var data1 = newOption.series[1].data;
      data0.shift();
      data0.push(Math.round(Math.random() * 1000));
      data1.shift();
      data1.push((Math.random() * 10 + 5).toFixed(1) - 0);
      newOption.xAxis[0].data.shift();
      newOption.xAxis[0].data.push(axisData);
      newOption.xAxis[1].data.shift();
      newOption.xAxis[1].data.push(count++);
      setOption(newOption);
    }

    (0, _react.useEffect)(function () {
      var timer = setInterval(function () {
        fetchNewData();
      }, 1000);
      return function () {
        return clearInterval(timer);
      };
    });
    return /*#__PURE__*/_react["default"].createElement(_echartsForReact["default"], {
      option: option,
      style: {
        height: 400
      }
    });
  };

  var _default;

  return _react["default"].createElement(Page);
},
    previewerProps: {"sources":{"_":{"tsx":"import React, { useState, useEffect } from 'react';\nimport ReactECharts from 'echarts-for-react';\nimport cloneDeep from 'lodash.clonedeep';\n\nconst Page: React.FC = () => {\n  const DEFAULT_OPTION = {\n    title: {\n      text: 'Hello Echarts-for-react.',\n    },\n    tooltip: {\n      trigger: 'axis',\n    },\n    legend: {\n      data: ['最新成交价', '预购队列'],\n    },\n    toolbox: {\n      show: true,\n      feature: {\n        dataView: { readOnly: false },\n        restore: {},\n        saveAsImage: {},\n      },\n    },\n    grid: {\n      top: 60,\n      left: 30,\n      right: 60,\n      bottom: 90,\n    },\n    dataZoom: {\n      show: false,\n      start: 0,\n      end: 100,\n    },\n    visualMap: {\n      show: false,\n      min: 0,\n      max: 1000,\n      color: [\n        '#BE002F',\n        '#F20C00',\n        '#F00056',\n        '#FF2D51',\n        '#FF2121',\n        '#FF4C00',\n        '#FF7500',\n        '#FF8936',\n        '#FFA400',\n        '#F0C239',\n        '#FFF143',\n        '#FAFF72',\n        '#C9DD22',\n        '#AFDD22',\n        '#9ED900',\n        '#00E500',\n        '#0EB83A',\n        '#0AA344',\n        '#0C8918',\n        '#057748',\n        '#177CB0',\n      ],\n    },\n    xAxis: [\n      {\n        type: 'category',\n        boundaryGap: true,\n        data: (function () {\n          let now = new Date();\n          let res = [];\n          let len = 50;\n          while (len--) {\n            res.unshift(now.toLocaleTimeString().replace(/^\\D*/, ''));\n            now = new Date(now - 2000);\n          }\n          return res;\n        })(),\n      },\n      {\n        type: 'category',\n        boundaryGap: true,\n        data: (function () {\n          let res = [];\n          let len = 50;\n          while (len--) {\n            res.push(50 - len + 1);\n          }\n          return res;\n        })(),\n      },\n    ],\n    yAxis: [\n      {\n        type: 'value',\n        scale: true,\n        name: '价格',\n        max: 20,\n        min: 0,\n        boundaryGap: [0.2, 0.2],\n      },\n      {\n        type: 'value',\n        scale: true,\n        name: '预购量',\n        max: 1200,\n        min: 0,\n        boundaryGap: [0.2, 0.2],\n      },\n    ],\n    series: [\n      {\n        name: '预购队列',\n        type: 'bar',\n        xAxisIndex: 1,\n        yAxisIndex: 1,\n        itemStyle: {\n          normal: {\n            barBorderRadius: 4,\n          },\n        },\n        animationEasing: 'elasticOut',\n        animationDelay: function (idx) {\n          return idx * 10;\n        },\n        animationDelayUpdate: function (idx) {\n          return idx * 10;\n        },\n        data: (function () {\n          let res = [];\n          let len = 50;\n          while (len--) {\n            res.push(Math.round(Math.random() * 1000));\n          }\n          return res;\n        })(),\n      },\n      {\n        name: '最新成交价',\n        type: 'line',\n        data: (function () {\n          let res = [];\n          let len = 0;\n          while (len < 50) {\n            res.push((Math.random() * 10 + 5).toFixed(1) - 0);\n            len++;\n          }\n          return res;\n        })(),\n      },\n    ],\n  };\n\n  let count;\n\n  const [option, setOption] = useState(DEFAULT_OPTION);\n\n  function fetchNewData() {\n    const axisData = new Date().toLocaleTimeString().replace(/^\\D*/, '');\n    const newOption = cloneDeep(option); // immutable\n    newOption.title.text = 'Hello Echarts-for-react.' + new Date().getSeconds();\n    const data0 = newOption.series[0].data;\n    const data1 = newOption.series[1].data;\n    data0.shift();\n    data0.push(Math.round(Math.random() * 1000));\n    data1.shift();\n    data1.push((Math.random() * 10 + 5).toFixed(1) - 0);\n\n    newOption.xAxis[0].data.shift();\n    newOption.xAxis[0].data.push(axisData);\n    newOption.xAxis[1].data.shift();\n    newOption.xAxis[1].data.push(count++);\n\n    setOption(newOption);\n  }\n\n  useEffect(() => {\n    const timer = setInterval(() => {\n      fetchNewData();\n    }, 1000);\n\n    return () => clearInterval(timer);\n  });\n\n  return <ReactECharts option={option} style={{ height: 400 }} />;\n};\n\nexport default Page;"}},"dependencies":{"react":{"version":"^15.0.0 || >=16.0.0"},"echarts-for-react":{"version":"3.0.6"},"lodash.clonedeep":{"version":"4.5.0"},"echarts":{"version":"^3.0.0 || ^4.0.0 || ^5.0.0 || ^6.0.0"}},"identifier":"dynamic-demo"},
  },
  'event-demo': {
    component: function DumiDemo() {
  var _interopRequireWildcard = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireWildcard.js")["default"];

  var _interopRequireDefault = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireDefault.js")["default"];

  var _slicedToArray2 = _interopRequireDefault(require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/esm/slicedToArray.js"));

  var _react = _interopRequireWildcard(require("react"));

  var _echartsForReact = _interopRequireDefault(require("echarts-for-react"));

  var Page = function Page() {
    var option = {
      title: {
        text: '某站点用户访问来源',
        subtext: '纯属虚构',
        x: 'center'
      },
      tooltip: {
        trigger: 'item',
        formatter: "{a} <br/>{b} : {c} ({d}%)"
      },
      legend: {
        orient: 'vertical',
        left: 'left',
        data: ['直接访问', '邮件营销', '联盟广告', '视频广告', '搜索引擎']
      },
      series: [{
        name: '访问来源',
        type: 'pie',
        radius: '55%',
        center: ['50%', '60%'],
        data: [{
          value: 335,
          name: '直接访问'
        }, {
          value: 310,
          name: '邮件营销'
        }, {
          value: 234,
          name: '联盟广告'
        }, {
          value: 135,
          name: '视频广告'
        }, {
          value: 1548,
          name: '搜索引擎'
        }],
        itemStyle: {
          emphasis: {
            shadowBlur: 10,
            shadowOffsetX: 0,
            shadowColor: 'rgba(0, 0, 0, 0.5)'
          }
        }
      }]
    };

    var _useState = (0, _react.useState)(0),
        _useState2 = (0, _slicedToArray2["default"])(_useState, 2),
        count = _useState2[0],
        setCount = _useState2[1];

    function onChartReady(echarts) {
      console.log('echarts is ready', echarts);
    }

    function onChartClick(param, echarts) {
      console.log(param, echarts);
      setCount(count + 1);
    }

    ;

    function onChartLegendselectchanged(param, echarts) {
      console.log(param, echarts);
    }

    ;
    return /*#__PURE__*/_react["default"].createElement(_react["default"].Fragment, null, /*#__PURE__*/_react["default"].createElement(_echartsForReact["default"], {
      option: option,
      style: {
        height: 400
      },
      onChartReady: onChartReady,
      onEvents: {
        'click': onChartClick,
        'legendselectchanged': onChartLegendselectchanged
      }
    }), /*#__PURE__*/_react["default"].createElement("div", null, "Click Count: ", count), /*#__PURE__*/_react["default"].createElement("div", null, "Open console, see the log detail."));
  };

  var _default;

  return _react["default"].createElement(Page);
},
    previewerProps: {"sources":{"_":{"tsx":"import React, { useState } from 'react';\nimport ReactECharts from 'echarts-for-react';\n\nconst Page: React.FC = () => {\n  const option = {\n    title : {\n      text: '某站点用户访问来源',\n      subtext: '纯属虚构',\n      x:'center'\n    },\n    tooltip : {\n      trigger: 'item',\n      formatter: \"{a} <br/>{b} : {c} ({d}%)\"\n    },\n    legend: {\n      orient: 'vertical',\n      left: 'left',\n      data: ['直接访问','邮件营销','联盟广告','视频广告','搜索引擎']\n    },\n    series : [\n      {\n      name: '访问来源',\n      type: 'pie',\n      radius : '55%',\n      center: ['50%', '60%'],\n      data:[\n        {value:335, name:'直接访问'},\n        {value:310, name:'邮件营销'},\n        {value:234, name:'联盟广告'},\n        {value:135, name:'视频广告'},\n        {value:1548, name:'搜索引擎'}\n      ],\n      itemStyle: {\n        emphasis: {\n        shadowBlur: 10,\n        shadowOffsetX: 0,\n        shadowColor: 'rgba(0, 0, 0, 0.5)'\n        }\n      }\n      }\n    ]\n  };\n\n  const [count, setCount] = useState(0);\n\n  function onChartReady(echarts) {\n    console.log('echarts is ready', echarts);\n  }\n\n  function onChartClick(param, echarts) {\n    console.log(param, echarts);\n    setCount(count + 1);\n  };\n\n  function onChartLegendselectchanged(param, echarts) {\n    console.log(param, echarts);\n  };\n\n  return (\n    <>\n      <ReactECharts\n        option={option}\n        style={{ height: 400 }}\n        onChartReady={onChartReady}\n        onEvents={{\n          'click': onChartClick,\n          'legendselectchanged': onChartLegendselectchanged\n         }}\n      />\n      <div>Click Count: {count}</div>\n      <div>Open console, see the log detail.</div>\n    </>\n  );\n};\n\nexport default Page;"}},"dependencies":{"react":{"version":"^15.0.0 || >=16.0.0"},"echarts-for-react":{"version":"3.0.6"},"echarts":{"version":"^3.0.0 || ^4.0.0 || ^5.0.0 || ^6.0.0"}},"identifier":"event-demo"},
  },
  'gl-demo': {
    component: function DumiDemo() {
  var _interopRequireDefault = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireDefault.js")["default"];

  var _react = _interopRequireDefault(require("react"));

  var _echartsForReact = _interopRequireDefault(require("echarts-for-react"));

  require("echarts-gl");

  var Page = function Page() {
    var option = {
      grid3D: {},
      xAxis3D: {},
      yAxis3D: {},
      zAxis3D: {},
      series: [{
        type: 'scatter3D',
        symbolSize: 50,
        data: [[-1, -1, -1], [0, 0, 0], [1, 1, 1]],
        itemStyle: {
          opacity: 1
        }
      }]
    };
    return /*#__PURE__*/_react["default"].createElement(_echartsForReact["default"], {
      option: option,
      style: {
        height: 400
      }
    });
  };

  var _default;

  return _react["default"].createElement(Page);
},
    previewerProps: {"sources":{"_":{"tsx":"import React from 'react';\nimport ReactECharts from 'echarts-for-react';\nimport 'echarts-gl';\n\nconst Page: React.FC = () => {\n  const option = {\n    grid3D: {},\n    xAxis3D: {},\n    yAxis3D: {},\n    zAxis3D: {},\n    series: [{\n      type: 'scatter3D',\n      symbolSize: 50,\n      data: [[-1, -1, -1], [0, 0, 0], [1, 1, 1]],\n      itemStyle: {\n        opacity: 1\n      }\n    }]\n  };\n\n  return <ReactECharts\n    option={option}\n    style={{ height: 400 }}\n  />;\n};\n\nexport default Page;"}},"dependencies":{"react":{"version":"^15.0.0 || >=16.0.0"},"echarts-for-react":{"version":"3.0.6"},"echarts-gl":{"version":"2.0.9"},"echarts":{"version":"^5.1.2"}},"identifier":"gl-demo"},
  },
  'graph-demo': {
    component: function DumiDemo() {
  var _interopRequireDefault = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireDefault.js")["default"];

  var _react = _interopRequireDefault(require("react"));

  var _echartsForReact = _interopRequireDefault(require("echarts-for-react"));

  var Page = function Page() {
    var webkitDep = {
      "type": "force",
      "categories": [{
        "name": "HTMLElement",
        "keyword": {},
        "base": "HTMLElement"
      }, {
        "name": "WebGL",
        "keyword": {},
        "base": "WebGLRenderingContext"
      }, {
        "name": "SVG",
        "keyword": {},
        "base": "SVGElement"
      }, {
        "name": "CSS",
        "keyword": {},
        "base": "CSSRule"
      }, {
        "name": "Other",
        "keyword": {}
      }],
      "nodes": [{
        "name": "AnalyserNode",
        "value": 1,
        "category": 4
      }, {
        "name": "AudioNode",
        "value": 1,
        "category": 4
      }, {
        "name": "Uint8Array",
        "value": 1,
        "category": 4
      }, {
        "name": "Float32Array",
        "value": 1,
        "category": 4
      }, {
        "name": "ArrayBuffer",
        "value": 1,
        "category": 4
      }, {
        "name": "ArrayBufferView",
        "value": 1,
        "category": 4
      }, {
        "name": "Attr",
        "value": 1,
        "category": 4
      }, {
        "name": "Node",
        "value": 1,
        "category": 4
      }, {
        "name": "Element",
        "value": 1,
        "category": 4
      }, {
        "name": "AudioBuffer",
        "value": 1,
        "category": 4
      }, {
        "name": "AudioBufferCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "AudioBufferSourceNode",
        "value": 1,
        "category": 4
      }, {
        "name": "AudioSourceNode",
        "value": 1,
        "category": 4
      }, {
        "name": "AudioGain",
        "value": 1,
        "category": 4
      }, {
        "name": "AudioParam",
        "value": 1,
        "category": 4
      }, {
        "name": "AudioContext",
        "value": 1,
        "category": 4
      }, {
        "name": "AudioDestinationNode",
        "value": 1,
        "category": 4
      }, {
        "name": "AudioListener",
        "value": 1,
        "category": 4
      }, {
        "name": "BiquadFilterNode",
        "value": 1,
        "category": 4
      }, {
        "name": "ChannelMergerNode",
        "value": 1,
        "category": 4
      }, {
        "name": "ChannelSplitterNode",
        "value": 1,
        "category": 4
      }, {
        "name": "ConvolverNode",
        "value": 1,
        "category": 4
      }, {
        "name": "DelayNode",
        "value": 1,
        "category": 4
      }, {
        "name": "DynamicsCompressorNode",
        "value": 1,
        "category": 4
      }, {
        "name": "GainNode",
        "value": 1,
        "category": 4
      }, {
        "name": "MediaElementAudioSourceNode",
        "value": 1,
        "category": 4
      }, {
        "name": "MediaStreamAudioDestinationNode",
        "value": 1,
        "category": 4
      }, {
        "name": "MediaStreamAudioSourceNode",
        "value": 1,
        "category": 4
      }, {
        "name": "OscillatorNode",
        "value": 1,
        "category": 4
      }, {
        "name": "PannerNode",
        "value": 1,
        "category": 4
      }, {
        "name": "ScriptProcessorNode",
        "value": 1,
        "category": 4
      }, {
        "name": "WaveShaperNode",
        "value": 1,
        "category": 4
      }, {
        "name": "WaveTable",
        "value": 1,
        "category": 4
      }, {
        "name": "CanvasRenderingContext",
        "value": 1,
        "category": 4
      }, {
        "name": "HTMLCanvasElement",
        "value": 1,
        "category": 0
      }, {
        "name": "CanvasRenderingContext2D",
        "value": 1,
        "category": 4
      }, {
        "name": "ImageData",
        "value": 1,
        "category": 4
      }, {
        "name": "CanvasGradient",
        "value": 1,
        "category": 4
      }, {
        "name": "CanvasPattern",
        "value": 1,
        "category": 4
      }, {
        "name": "HTMLImageElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLVideoElement",
        "value": 1,
        "category": 0
      }, {
        "name": "TextMetrics",
        "value": 1,
        "category": 4
      }, {
        "name": "CDATASection",
        "value": 1,
        "category": 4
      }, {
        "name": "Text",
        "value": 1,
        "category": 4
      }, {
        "name": "CharacterData",
        "value": 1,
        "category": 4
      }, {
        "name": "ClientRectList",
        "value": 1,
        "category": 4
      }, {
        "name": "ClientRect",
        "value": 1,
        "category": 4
      }, {
        "name": "Clipboard",
        "value": 1,
        "category": 4
      }, {
        "name": "FileList",
        "value": 1,
        "category": 4
      }, {
        "name": "DataTransferItemList",
        "value": 1,
        "category": 4
      }, {
        "name": "Comment",
        "value": 1,
        "category": 4
      }, {
        "name": "Console",
        "value": 1,
        "category": 4
      }, {
        "name": "MemoryInfo",
        "value": 1,
        "category": 4
      }, {
        "name": "Crypto",
        "value": 1,
        "category": 4
      }, {
        "name": "CSSCharsetRule",
        "value": 1,
        "category": 3
      }, {
        "name": "CSSRule",
        "value": 3,
        "category": 3
      }, {
        "name": "CSSFontFaceRule",
        "value": 1,
        "category": 3
      }, {
        "name": "CSSStyleDeclaration",
        "value": 1,
        "category": 3
      }, {
        "name": "CSSImportRule",
        "value": 1,
        "category": 3
      }, {
        "name": "MediaList",
        "value": 1,
        "category": 4
      }, {
        "name": "CSSStyleSheet",
        "value": 1,
        "category": 3
      }, {
        "name": "CSSMediaRule",
        "value": 1,
        "category": 3
      }, {
        "name": "CSSRuleList",
        "value": 1,
        "category": 3
      }, {
        "name": "CSSPageRule",
        "value": 1,
        "category": 3
      }, {
        "name": "CSSPrimitiveValue",
        "value": 1,
        "category": 3
      }, {
        "name": "CSSValue",
        "value": 1,
        "category": 3
      }, {
        "name": "Counter",
        "value": 1,
        "category": 4
      }, {
        "name": "RGBColor",
        "value": 1,
        "category": 4
      }, {
        "name": "Rect",
        "value": 1,
        "category": 4
      }, {
        "name": "CSSStyleRule",
        "value": 1,
        "category": 3
      }, {
        "name": "StyleSheet",
        "value": 1,
        "category": 4
      }, {
        "name": "CSSUnknownRule",
        "value": 1,
        "category": 3
      }, {
        "name": "CSSValueList",
        "value": 1,
        "category": 3
      }, {
        "name": "Database",
        "value": 1,
        "category": 4
      }, {
        "name": "SQLTransactionCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "DatabaseCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "DatabaseSync",
        "value": 1,
        "category": 4
      }, {
        "name": "SQLTransactionSyncCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "DataTransferItem",
        "value": 1,
        "category": 4
      }, {
        "name": "StringCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "Entry",
        "value": 1,
        "category": 4
      }, {
        "name": "File",
        "value": 1,
        "category": 4
      }, {
        "name": "DataView",
        "value": 1,
        "category": 4
      }, {
        "name": "DedicatedWorkerContext",
        "value": 1,
        "category": 4
      }, {
        "name": "WorkerContext",
        "value": 1,
        "category": 4
      }, {
        "name": "DirectoryEntry",
        "value": 1,
        "category": 4
      }, {
        "name": "DirectoryReader",
        "value": 1,
        "category": 4
      }, {
        "name": "VoidCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "DirectoryEntrySync",
        "value": 1,
        "category": 4
      }, {
        "name": "EntrySync",
        "value": 1,
        "category": 4
      }, {
        "name": "DirectoryReaderSync",
        "value": 1,
        "category": 4
      }, {
        "name": "FileEntrySync",
        "value": 1,
        "category": 4
      }, {
        "name": "EntriesCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "EntryArraySync",
        "value": 1,
        "category": 4
      }, {
        "name": "DocumentFragment",
        "value": 1,
        "category": 4
      }, {
        "name": "NodeList",
        "value": 1,
        "category": 4
      }, {
        "name": "DocumentType",
        "value": 1,
        "category": 4
      }, {
        "name": "NamedNodeMap",
        "value": 1,
        "category": 4
      }, {
        "name": "DOMFileSystem",
        "value": 1,
        "category": 4
      }, {
        "name": "DOMFileSystemSync",
        "value": 1,
        "category": 4
      }, {
        "name": "DOMImplementation",
        "value": 1,
        "category": 4
      }, {
        "name": "HTMLDocument",
        "value": 1,
        "category": 0
      }, {
        "name": "DOMMimeType",
        "value": 1,
        "category": 4
      }, {
        "name": "DOMPlugin",
        "value": 1,
        "category": 4
      }, {
        "name": "DOMMimeTypeArray",
        "value": 1,
        "category": 4
      }, {
        "name": "DOMPluginArray",
        "value": 1,
        "category": 4
      }, {
        "name": "DOMSelection",
        "value": 1,
        "category": 4
      }, {
        "name": "Range",
        "value": 1,
        "category": 4
      }, {
        "name": "DOMSettableTokenList",
        "value": 1,
        "category": 4
      }, {
        "name": "DOMTokenList",
        "value": 1,
        "category": 4
      }, {
        "name": "DOMStringMap",
        "value": 1,
        "category": 4
      }, {
        "name": "ShadowRoot",
        "value": 1,
        "category": 4
      }, {
        "name": "Entity",
        "value": 1,
        "category": 4
      }, {
        "name": "EntityReference",
        "value": 1,
        "category": 4
      }, {
        "name": "EntryArray",
        "value": 1,
        "category": 4
      }, {
        "name": "MetadataCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "EntryCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "Metadata",
        "value": 1,
        "category": 4
      }, {
        "name": "ErrorCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "FileError",
        "value": 1,
        "category": 4
      }, {
        "name": "FileCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "FileEntry",
        "value": 1,
        "category": 4
      }, {
        "name": "FileWriterCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "FileWriterSync",
        "value": 1,
        "category": 4
      }, {
        "name": "FileReader",
        "value": 1,
        "category": 4
      }, {
        "name": "FileReaderSync",
        "value": 1,
        "category": 4
      }, {
        "name": "FileSystemCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "FileWriter",
        "value": 1,
        "category": 4
      }, {
        "name": "Float64Array",
        "value": 1,
        "category": 4
      }, {
        "name": "GamepadList",
        "value": 1,
        "category": 4
      }, {
        "name": "Gamepad",
        "value": 1,
        "category": 4
      }, {
        "name": "Geolocation",
        "value": 1,
        "category": 4
      }, {
        "name": "PositionCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "Geoposition",
        "value": 1,
        "category": 4
      }, {
        "name": "Coordinates",
        "value": 1,
        "category": 4
      }, {
        "name": "HTMLAllCollection",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLAnchorElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLElement",
        "value": 3,
        "category": 0
      }, {
        "name": "HTMLAppletElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLAreaElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLAudioElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLMediaElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLBaseElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLBaseFontElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLBodyElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLBRElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLButtonElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLFormElement",
        "value": 1,
        "category": 0
      }, {
        "name": "ValidityState",
        "value": 1,
        "category": 4
      }, {
        "name": "HTMLCollection",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLContentElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLDataListElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLDetailsElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLDirectoryElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLDivElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLDListElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLEmbedElement",
        "value": 1,
        "category": 0
      }, {
        "name": "SVGDocument",
        "value": 1,
        "category": 2
      }, {
        "name": "HTMLFieldSetElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLFontElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLFormControlsCollection",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLFrameElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLFrameSetElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLHeadElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLHeadingElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLHRElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLHtmlElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLIFrameElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLInputElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLKeygenElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLLabelElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLLegendElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLLIElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLLinkElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLMapElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLMarqueeElement",
        "value": 1,
        "category": 0
      }, {
        "name": "TimeRanges",
        "value": 1,
        "category": 4
      }, {
        "name": "MediaController",
        "value": 1,
        "category": 4
      }, {
        "name": "MediaError",
        "value": 1,
        "category": 4
      }, {
        "name": "TextTrackList",
        "value": 1,
        "category": 4
      }, {
        "name": "TextTrack",
        "value": 1,
        "category": 4
      }, {
        "name": "HTMLMenuElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLMetaElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLMeterElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLModElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLObjectElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLOListElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLOptGroupElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLOptionElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLOptionsCollection",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLOutputElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLParagraphElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLParamElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLPreElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLProgressElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLQuoteElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLScriptElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLSelectElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLShadowElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLSourceElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLSpanElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLStyleElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLTableCaptionElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLTableCellElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLTableColElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLTableElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLTableSectionElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLTableRowElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLTextAreaElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLTitleElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLTrackElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLUListElement",
        "value": 1,
        "category": 0
      }, {
        "name": "HTMLUnknownElement",
        "value": 1,
        "category": 0
      }, {
        "name": "IDBCursor",
        "value": 1,
        "category": 4
      }, {
        "name": "IDBAny",
        "value": 1,
        "category": 4
      }, {
        "name": "IDBKey",
        "value": 1,
        "category": 4
      }, {
        "name": "IDBRequest",
        "value": 1,
        "category": 4
      }, {
        "name": "IDBCursorWithValue",
        "value": 1,
        "category": 4
      }, {
        "name": "IDBDatabase",
        "value": 1,
        "category": 4
      }, {
        "name": "DOMStringList",
        "value": 1,
        "category": 4
      }, {
        "name": "IDBObjectStore",
        "value": 1,
        "category": 4
      }, {
        "name": "IDBTransaction",
        "value": 1,
        "category": 4
      }, {
        "name": "IDBFactory",
        "value": 1,
        "category": 4
      }, {
        "name": "IDBVersionChangeRequest",
        "value": 1,
        "category": 4
      }, {
        "name": "IDBOpenDBRequest",
        "value": 1,
        "category": 4
      }, {
        "name": "IDBIndex",
        "value": 1,
        "category": 4
      }, {
        "name": "IDBKeyRange",
        "value": 1,
        "category": 4
      }, {
        "name": "DOMError",
        "value": 1,
        "category": 4
      }, {
        "name": "Int16Array",
        "value": 1,
        "category": 4
      }, {
        "name": "Int32Array",
        "value": 1,
        "category": 4
      }, {
        "name": "Int8Array",
        "value": 1,
        "category": 4
      }, {
        "name": "JavaScriptCallFrame",
        "value": 1,
        "category": 4
      }, {
        "name": "LocalMediaStream",
        "value": 1,
        "category": 4
      }, {
        "name": "MediaStream",
        "value": 1,
        "category": 4
      }, {
        "name": "Location",
        "value": 1,
        "category": 4
      }, {
        "name": "MediaQueryList",
        "value": 1,
        "category": 4
      }, {
        "name": "MediaQueryListListener",
        "value": 1,
        "category": 4
      }, {
        "name": "MediaSource",
        "value": 1,
        "category": 4
      }, {
        "name": "SourceBufferList",
        "value": 1,
        "category": 4
      }, {
        "name": "SourceBuffer",
        "value": 1,
        "category": 4
      }, {
        "name": "MediaStreamTrackList",
        "value": 1,
        "category": 4
      }, {
        "name": "MediaStreamList",
        "value": 1,
        "category": 4
      }, {
        "name": "MediaStreamTrack",
        "value": 1,
        "category": 4
      }, {
        "name": "MessageChannel",
        "value": 1,
        "category": 4
      }, {
        "name": "MessagePort",
        "value": 1,
        "category": 4
      }, {
        "name": "MutationObserver",
        "value": 1,
        "category": 4
      }, {
        "name": "MutationRecord",
        "value": 1,
        "category": 4
      }, {
        "name": "Navigator",
        "value": 1,
        "category": 4
      }, {
        "name": "BatteryManager",
        "value": 1,
        "category": 4
      }, {
        "name": "NavigatorUserMediaErrorCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "NavigatorUserMediaError",
        "value": 1,
        "category": 4
      }, {
        "name": "NavigatorUserMediaSuccessCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "NodeFilter",
        "value": 1,
        "category": 4
      }, {
        "name": "NodeIterator",
        "value": 1,
        "category": 4
      }, {
        "name": "Notation",
        "value": 1,
        "category": 4
      }, {
        "name": "Notification",
        "value": 1,
        "category": 4
      }, {
        "name": "NotificationPermissionCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "NotificationCenter",
        "value": 1,
        "category": 4
      }, {
        "name": "OESVertexArrayObject",
        "value": 1,
        "category": 4
      }, {
        "name": "WebGLVertexArrayObjectOES",
        "value": 1,
        "category": 1
      }, {
        "name": "Performance",
        "value": 1,
        "category": 4
      }, {
        "name": "PerformanceNavigation",
        "value": 1,
        "category": 4
      }, {
        "name": "PerformanceTiming",
        "value": 1,
        "category": 4
      }, {
        "name": "PositionErrorCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "PositionError",
        "value": 1,
        "category": 4
      }, {
        "name": "ProcessingInstruction",
        "value": 1,
        "category": 4
      }, {
        "name": "RadioNodeList",
        "value": 1,
        "category": 4
      }, {
        "name": "RTCDataChannel",
        "value": 1,
        "category": 4
      }, {
        "name": "RTCPeerConnection",
        "value": 1,
        "category": 4
      }, {
        "name": "RTCSessionDescription",
        "value": 1,
        "category": 4
      }, {
        "name": "RTCIceCandidate",
        "value": 1,
        "category": 4
      }, {
        "name": "RTCSessionDescriptionCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "RTCStatsCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "RTCStatsResponse",
        "value": 1,
        "category": 4
      }, {
        "name": "RTCStatsReport",
        "value": 1,
        "category": 4
      }, {
        "name": "RTCStatsElement",
        "value": 1,
        "category": 4
      }, {
        "name": "ScriptProfile",
        "value": 1,
        "category": 4
      }, {
        "name": "ScriptProfileNode",
        "value": 1,
        "category": 4
      }, {
        "name": "SharedWorker",
        "value": 1,
        "category": 4
      }, {
        "name": "AbstractWorker",
        "value": 1,
        "category": 4
      }, {
        "name": "SharedWorkerContext",
        "value": 1,
        "category": 4
      }, {
        "name": "SpeechGrammarList",
        "value": 1,
        "category": 4
      }, {
        "name": "SpeechGrammar",
        "value": 1,
        "category": 4
      }, {
        "name": "SpeechInputResultList",
        "value": 1,
        "category": 4
      }, {
        "name": "SpeechInputResult",
        "value": 1,
        "category": 4
      }, {
        "name": "SpeechRecognition",
        "value": 1,
        "category": 4
      }, {
        "name": "SpeechRecognitionResult",
        "value": 1,
        "category": 4
      }, {
        "name": "SpeechRecognitionAlternative",
        "value": 1,
        "category": 4
      }, {
        "name": "SpeechRecognitionResultList",
        "value": 1,
        "category": 4
      }, {
        "name": "SQLResultSet",
        "value": 1,
        "category": 4
      }, {
        "name": "SQLResultSetRowList",
        "value": 1,
        "category": 4
      }, {
        "name": "SQLStatementCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "SQLTransaction",
        "value": 1,
        "category": 4
      }, {
        "name": "SQLStatementErrorCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "SQLTransactionErrorCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "SQLError",
        "value": 1,
        "category": 4
      }, {
        "name": "SQLTransactionSync",
        "value": 1,
        "category": 4
      }, {
        "name": "StorageInfo",
        "value": 1,
        "category": 4
      }, {
        "name": "StorageInfoUsageCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "StorageInfoQuotaCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "StorageInfoErrorCallback",
        "value": 1,
        "category": 4
      }, {
        "name": "DOMCoreException",
        "value": 1,
        "category": 4
      }, {
        "name": "StyleSheetList",
        "value": 1,
        "category": 4
      }, {
        "name": "SVGAElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGTransformable",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimatedString",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAltGlyphDefElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGElement",
        "value": 3,
        "category": 2
      }, {
        "name": "SVGAltGlyphElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGURIReference",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAltGlyphItemElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimateColorElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimationElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimatedAngle",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAngle",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimatedLength",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGLength",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimatedLengthList",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGLengthList",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimatedNumberList",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGNumberList",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimatedPreserveAspectRatio",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPreserveAspectRatio",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimatedRect",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGRect",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimatedTransformList",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGTransformList",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimateElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimateMotionElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimateTransformElement",
        "value": 1,
        "category": 2
      }, {
        "name": "ElementTimeControl",
        "value": 1,
        "category": 4
      }, {
        "name": "SVGCircleElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGClipPathElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimatedEnumeration",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGColor",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGComponentTransferFunctionElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimatedNumber",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGCursorElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGExternalResourcesRequired",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGDefsElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGDescElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGStylable",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGSVGElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGElementInstance",
        "value": 1,
        "category": 2
      }, {
        "name": "EventTarget",
        "value": 1,
        "category": 4
      }, {
        "name": "SVGElementInstanceList",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGUseElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGEllipseElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimatedBoolean",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEBlendElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFilterPrimitiveStandardAttributes",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEColorMatrixElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEComponentTransferElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFECompositeElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEConvolveMatrixElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGAnimatedInteger",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEDiffuseLightingElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEDisplacementMapElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEDistantLightElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEDropShadowElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEFloodElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEFuncAElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEFuncBElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEFuncGElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEFuncRElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEGaussianBlurElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEImageElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEMergeElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEMergeNodeElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEMorphologyElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEOffsetElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFEPointLightElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFESpecularLightingElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFESpotLightElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFETileElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFETurbulenceElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFilterElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFitToViewBox",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFontElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFontFaceElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFontFaceFormatElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFontFaceNameElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFontFaceSrcElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGFontFaceUriElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGForeignObjectElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGGElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGGlyphElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGGlyphRefElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGGradientElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGHKernElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGImageElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGLinearGradientElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGLineElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGLocatable",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGMatrix",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGMarkerElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGMaskElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGMetadataElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGMissingGlyphElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGMPathElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGNumber",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPaint",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegList",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegArcAbs",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegArcRel",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegClosePath",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegCurvetoCubicAbs",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegCurvetoCubicRel",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegCurvetoCubicSmoothAbs",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegCurvetoCubicSmoothRel",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegCurvetoQuadraticAbs",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegCurvetoQuadraticRel",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegCurvetoQuadraticSmoothAbs",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegCurvetoQuadraticSmoothRel",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegLinetoAbs",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegLinetoHorizontalAbs",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegLinetoHorizontalRel",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegLinetoRel",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegLinetoVerticalAbs",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegLinetoVerticalRel",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegMovetoAbs",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSegMovetoRel",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPoint",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPathSeg",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPatternElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPointList",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPolygonElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGPolylineElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGRadialGradientElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGRectElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGScriptElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGSetElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGStopElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGStyleElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGLangSpace",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGZoomAndPan",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGViewSpec",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGTransform",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGSwitchElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGSymbolElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGTests",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGStringList",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGTextContentElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGTextElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGTextPathElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGTextPositioningElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGTitleElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGTRefElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGTSpanElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGViewElement",
        "value": 1,
        "category": 2
      }, {
        "name": "SVGVKernElement",
        "value": 1,
        "category": 2
      }, {
        "name": "TextTrackCueList",
        "value": 1,
        "category": 4
      }, {
        "name": "TextTrackCue",
        "value": 1,
        "category": 4
      }, {
        "name": "Touch",
        "value": 1,
        "category": 4
      }, {
        "name": "TouchList",
        "value": 1,
        "category": 4
      }, {
        "name": "TreeWalker",
        "value": 1,
        "category": 4
      }, {
        "name": "Uint16Array",
        "value": 1,
        "category": 4
      }, {
        "name": "Uint32Array",
        "value": 1,
        "category": 4
      }, {
        "name": "Uint8ClampedArray",
        "value": 1,
        "category": 4
      }, {
        "name": "WebGLRenderingContext",
        "value": 3,
        "category": 1
      }, {
        "name": "WebGLProgram",
        "value": 1,
        "category": 1
      }, {
        "name": "WebGLBuffer",
        "value": 1,
        "category": 1
      }, {
        "name": "WebGLFramebuffer",
        "value": 1,
        "category": 1
      }, {
        "name": "WebGLRenderbuffer",
        "value": 1,
        "category": 1
      }, {
        "name": "WebGLTexture",
        "value": 1,
        "category": 1
      }, {
        "name": "WebGLShader",
        "value": 1,
        "category": 1
      }, {
        "name": "WebGLActiveInfo",
        "value": 1,
        "category": 1
      }, {
        "name": "WebGLContextAttributes",
        "value": 1,
        "category": 1
      }, {
        "name": "WebGLShaderPrecisionFormat",
        "value": 1,
        "category": 1
      }, {
        "name": "WebGLUniformLocation",
        "value": 1,
        "category": 1
      }, {
        "name": "WebKitAnimationList",
        "value": 1,
        "category": 4
      }, {
        "name": "WebKitAnimation",
        "value": 1,
        "category": 4
      }, {
        "name": "WebKitCSSFilterValue",
        "value": 1,
        "category": 4
      }, {
        "name": "WebKitCSSKeyframeRule",
        "value": 1,
        "category": 4
      }, {
        "name": "WebKitCSSKeyframesRule",
        "value": 1,
        "category": 4
      }, {
        "name": "WebKitCSSMatrix",
        "value": 1,
        "category": 4
      }, {
        "name": "WebKitCSSMixFunctionValue",
        "value": 1,
        "category": 4
      }, {
        "name": "WebKitCSSTransformValue",
        "value": 1,
        "category": 4
      }, {
        "name": "WebKitNamedFlow",
        "value": 1,
        "category": 4
      }, {
        "name": "WebSocket",
        "value": 1,
        "category": 4
      }, {
        "name": "Worker",
        "value": 1,
        "category": 4
      }, {
        "name": "WorkerLocation",
        "value": 1,
        "category": 4
      }, {
        "name": "WorkerNavigator",
        "value": 1,
        "category": 4
      }, {
        "name": "XMLHttpRequest",
        "value": 1,
        "category": 4
      }, {
        "name": "XMLHttpRequestUpload",
        "value": 1,
        "category": 4
      }, {
        "name": "DOMFormData",
        "value": 1,
        "category": 4
      }, {
        "name": "XPathEvaluator",
        "value": 1,
        "category": 4
      }, {
        "name": "XPathExpression",
        "value": 1,
        "category": 4
      }, {
        "name": "XPathNSResolver",
        "value": 1,
        "category": 4
      }, {
        "name": "XPathResult",
        "value": 1,
        "category": 4
      }, {
        "name": "XSLTProcessor",
        "value": 1,
        "category": 4
      }],
      "links": [{
        "source": 0,
        "target": 1
      }, {
        "source": 0,
        "target": 2
      }, {
        "source": 0,
        "target": 3
      }, {
        "source": 4,
        "target": 4
      }, {
        "source": 5,
        "target": 4
      }, {
        "source": 6,
        "target": 7
      }, {
        "source": 6,
        "target": 8
      }, {
        "source": 9,
        "target": 3
      }, {
        "source": 10,
        "target": 9
      }, {
        "source": 11,
        "target": 12
      }, {
        "source": 11,
        "target": 9
      }, {
        "source": 11,
        "target": 13
      }, {
        "source": 11,
        "target": 14
      }, {
        "source": 15,
        "target": 16
      }, {
        "source": 15,
        "target": 17
      }, {
        "source": 15,
        "target": 0
      }, {
        "source": 15,
        "target": 18
      }, {
        "source": 15,
        "target": 9
      }, {
        "source": 15,
        "target": 11
      }, {
        "source": 15,
        "target": 19
      }, {
        "source": 15,
        "target": 20
      }, {
        "source": 15,
        "target": 21
      }, {
        "source": 15,
        "target": 22
      }, {
        "source": 15,
        "target": 23
      }, {
        "source": 15,
        "target": 24
      }, {
        "source": 15,
        "target": 25
      }, {
        "source": 15,
        "target": 26
      }, {
        "source": 15,
        "target": 27
      }, {
        "source": 15,
        "target": 28
      }, {
        "source": 15,
        "target": 29
      }, {
        "source": 15,
        "target": 30
      }, {
        "source": 15,
        "target": 31
      }, {
        "source": 15,
        "target": 32
      }, {
        "source": 15,
        "target": 4
      }, {
        "source": 16,
        "target": 1
      }, {
        "source": 13,
        "target": 14
      }, {
        "source": 1,
        "target": 15
      }, {
        "source": 1,
        "target": 1
      }, {
        "source": 1,
        "target": 14
      }, {
        "source": 14,
        "target": 3
      }, {
        "source": 12,
        "target": 1
      }, {
        "source": 18,
        "target": 1
      }, {
        "source": 18,
        "target": 14
      }, {
        "source": 18,
        "target": 3
      }, {
        "source": 33,
        "target": 34
      }, {
        "source": 35,
        "target": 33
      }, {
        "source": 35,
        "target": 36
      }, {
        "source": 35,
        "target": 37
      }, {
        "source": 35,
        "target": 38
      }, {
        "source": 35,
        "target": 39
      }, {
        "source": 35,
        "target": 34
      }, {
        "source": 35,
        "target": 40
      }, {
        "source": 35,
        "target": 41
      }, {
        "source": 42,
        "target": 43
      }, {
        "source": 19,
        "target": 1
      }, {
        "source": 20,
        "target": 1
      }, {
        "source": 44,
        "target": 7
      }, {
        "source": 45,
        "target": 46
      }, {
        "source": 47,
        "target": 48
      }, {
        "source": 47,
        "target": 49
      }, {
        "source": 47,
        "target": 39
      }, {
        "source": 50,
        "target": 44
      }, {
        "source": 51,
        "target": 52
      }, {
        "source": 21,
        "target": 1
      }, {
        "source": 21,
        "target": 9
      }, {
        "source": 53,
        "target": 5
      }, {
        "source": 54,
        "target": 55
      }, {
        "source": 56,
        "target": 55
      }, {
        "source": 56,
        "target": 57
      }, {
        "source": 58,
        "target": 55
      }, {
        "source": 58,
        "target": 59
      }, {
        "source": 58,
        "target": 60
      }, {
        "source": 61,
        "target": 55
      }, {
        "source": 61,
        "target": 62
      }, {
        "source": 61,
        "target": 59
      }, {
        "source": 63,
        "target": 55
      }, {
        "source": 63,
        "target": 57
      }, {
        "source": 64,
        "target": 65
      }, {
        "source": 64,
        "target": 66
      }, {
        "source": 64,
        "target": 67
      }, {
        "source": 64,
        "target": 68
      }, {
        "source": 55,
        "target": 55
      }, {
        "source": 55,
        "target": 60
      }, {
        "source": 62,
        "target": 55
      }, {
        "source": 57,
        "target": 55
      }, {
        "source": 57,
        "target": 65
      }, {
        "source": 69,
        "target": 55
      }, {
        "source": 69,
        "target": 57
      }, {
        "source": 60,
        "target": 70
      }, {
        "source": 60,
        "target": 62
      }, {
        "source": 60,
        "target": 55
      }, {
        "source": 71,
        "target": 55
      }, {
        "source": 72,
        "target": 65
      }, {
        "source": 73,
        "target": 74
      }, {
        "source": 75,
        "target": 73
      }, {
        "source": 75,
        "target": 76
      }, {
        "source": 76,
        "target": 77
      }, {
        "source": 78,
        "target": 79
      }, {
        "source": 78,
        "target": 80
      }, {
        "source": 49,
        "target": 81
      }, {
        "source": 49,
        "target": 78
      }, {
        "source": 82,
        "target": 5
      }, {
        "source": 83,
        "target": 84
      }, {
        "source": 22,
        "target": 1
      }, {
        "source": 22,
        "target": 14
      }, {
        "source": 85,
        "target": 80
      }, {
        "source": 85,
        "target": 86
      }, {
        "source": 85,
        "target": 87
      }, {
        "source": 88,
        "target": 89
      }, {
        "source": 88,
        "target": 90
      }, {
        "source": 88,
        "target": 88
      }, {
        "source": 88,
        "target": 91
      }, {
        "source": 86,
        "target": 92
      }, {
        "source": 90,
        "target": 93
      }, {
        "source": 94,
        "target": 7
      }, {
        "source": 94,
        "target": 8
      }, {
        "source": 94,
        "target": 95
      }, {
        "source": 96,
        "target": 7
      }, {
        "source": 96,
        "target": 97
      }, {
        "source": 98,
        "target": 85
      }, {
        "source": 99,
        "target": 88
      }, {
        "source": 100,
        "target": 60
      }, {
        "source": 100,
        "target": 96
      }, {
        "source": 100,
        "target": 101
      }, {
        "source": 102,
        "target": 103
      }, {
        "source": 104,
        "target": 102
      }, {
        "source": 103,
        "target": 102
      }, {
        "source": 105,
        "target": 103
      }, {
        "source": 106,
        "target": 7
      }, {
        "source": 106,
        "target": 107
      }, {
        "source": 108,
        "target": 109
      }, {
        "source": 23,
        "target": 1
      }, {
        "source": 23,
        "target": 14
      }, {
        "source": 8,
        "target": 7
      }, {
        "source": 8,
        "target": 109
      }, {
        "source": 8,
        "target": 110
      }, {
        "source": 8,
        "target": 8
      }, {
        "source": 8,
        "target": 57
      }, {
        "source": 8,
        "target": 6
      }, {
        "source": 8,
        "target": 46
      }, {
        "source": 8,
        "target": 45
      }, {
        "source": 8,
        "target": 95
      }, {
        "source": 8,
        "target": 111
      }, {
        "source": 112,
        "target": 7
      }, {
        "source": 113,
        "target": 7
      }, {
        "source": 92,
        "target": 114
      }, {
        "source": 80,
        "target": 98
      }, {
        "source": 80,
        "target": 85
      }, {
        "source": 80,
        "target": 115
      }, {
        "source": 80,
        "target": 116
      }, {
        "source": 80,
        "target": 87
      }, {
        "source": 114,
        "target": 80
      }, {
        "source": 93,
        "target": 89
      }, {
        "source": 116,
        "target": 80
      }, {
        "source": 89,
        "target": 99
      }, {
        "source": 89,
        "target": 89
      }, {
        "source": 89,
        "target": 117
      }, {
        "source": 89,
        "target": 88
      }, {
        "source": 118,
        "target": 119
      }, {
        "source": 120,
        "target": 81
      }, {
        "source": 121,
        "target": 80
      }, {
        "source": 121,
        "target": 122
      }, {
        "source": 121,
        "target": 120
      }, {
        "source": 91,
        "target": 89
      }, {
        "source": 91,
        "target": 123
      }, {
        "source": 91,
        "target": 81
      }, {
        "source": 48,
        "target": 81
      }, {
        "source": 124,
        "target": 119
      }, {
        "source": 125,
        "target": 4
      }, {
        "source": 126,
        "target": 98
      }, {
        "source": 127,
        "target": 119
      }, {
        "source": 122,
        "target": 127
      }, {
        "source": 3,
        "target": 5
      }, {
        "source": 3,
        "target": 3
      }, {
        "source": 128,
        "target": 5
      }, {
        "source": 128,
        "target": 128
      }, {
        "source": 24,
        "target": 1
      }, {
        "source": 24,
        "target": 13
      }, {
        "source": 129,
        "target": 130
      }, {
        "source": 131,
        "target": 132
      }, {
        "source": 133,
        "target": 134
      }, {
        "source": 135,
        "target": 7
      }, {
        "source": 135,
        "target": 95
      }, {
        "source": 136,
        "target": 137
      }, {
        "source": 138,
        "target": 137
      }, {
        "source": 139,
        "target": 137
      }, {
        "source": 140,
        "target": 141
      }, {
        "source": 142,
        "target": 137
      }, {
        "source": 143,
        "target": 137
      }, {
        "source": 144,
        "target": 137
      }, {
        "source": 145,
        "target": 137
      }, {
        "source": 146,
        "target": 137
      }, {
        "source": 146,
        "target": 147
      }, {
        "source": 146,
        "target": 95
      }, {
        "source": 146,
        "target": 148
      }, {
        "source": 34,
        "target": 137
      }, {
        "source": 149,
        "target": 7
      }, {
        "source": 150,
        "target": 137
      }, {
        "source": 150,
        "target": 95
      }, {
        "source": 151,
        "target": 137
      }, {
        "source": 151,
        "target": 149
      }, {
        "source": 152,
        "target": 137
      }, {
        "source": 153,
        "target": 137
      }, {
        "source": 154,
        "target": 137
      }, {
        "source": 155,
        "target": 137
      }, {
        "source": 101,
        "target": 8
      }, {
        "source": 101,
        "target": 135
      }, {
        "source": 101,
        "target": 149
      }, {
        "source": 137,
        "target": 8
      }, {
        "source": 137,
        "target": 149
      }, {
        "source": 156,
        "target": 137
      }, {
        "source": 156,
        "target": 157
      }, {
        "source": 158,
        "target": 137
      }, {
        "source": 158,
        "target": 149
      }, {
        "source": 158,
        "target": 147
      }, {
        "source": 158,
        "target": 148
      }, {
        "source": 159,
        "target": 137
      }, {
        "source": 160,
        "target": 149
      }, {
        "source": 160,
        "target": 7
      }, {
        "source": 147,
        "target": 137
      }, {
        "source": 147,
        "target": 149
      }, {
        "source": 161,
        "target": 137
      }, {
        "source": 161,
        "target": 157
      }, {
        "source": 162,
        "target": 137
      }, {
        "source": 163,
        "target": 137
      }, {
        "source": 164,
        "target": 137
      }, {
        "source": 165,
        "target": 137
      }, {
        "source": 166,
        "target": 137
      }, {
        "source": 167,
        "target": 137
      }, {
        "source": 167,
        "target": 157
      }, {
        "source": 39,
        "target": 137
      }, {
        "source": 168,
        "target": 137
      }, {
        "source": 168,
        "target": 48
      }, {
        "source": 168,
        "target": 147
      }, {
        "source": 168,
        "target": 95
      }, {
        "source": 168,
        "target": 148
      }, {
        "source": 168,
        "target": 114
      }, {
        "source": 169,
        "target": 137
      }, {
        "source": 169,
        "target": 147
      }, {
        "source": 169,
        "target": 95
      }, {
        "source": 169,
        "target": 148
      }, {
        "source": 170,
        "target": 137
      }, {
        "source": 170,
        "target": 147
      }, {
        "source": 171,
        "target": 137
      }, {
        "source": 171,
        "target": 147
      }, {
        "source": 172,
        "target": 137
      }, {
        "source": 173,
        "target": 137
      }, {
        "source": 173,
        "target": 70
      }, {
        "source": 173,
        "target": 108
      }, {
        "source": 174,
        "target": 137
      }, {
        "source": 174,
        "target": 149
      }, {
        "source": 175,
        "target": 137
      }, {
        "source": 141,
        "target": 137
      }, {
        "source": 141,
        "target": 176
      }, {
        "source": 141,
        "target": 177
      }, {
        "source": 141,
        "target": 178
      }, {
        "source": 141,
        "target": 179
      }, {
        "source": 141,
        "target": 180
      }, {
        "source": 181,
        "target": 137
      }, {
        "source": 182,
        "target": 137
      }, {
        "source": 183,
        "target": 137
      }, {
        "source": 183,
        "target": 95
      }, {
        "source": 184,
        "target": 137
      }, {
        "source": 185,
        "target": 137
      }, {
        "source": 185,
        "target": 147
      }, {
        "source": 185,
        "target": 148
      }, {
        "source": 185,
        "target": 157
      }, {
        "source": 186,
        "target": 137
      }, {
        "source": 187,
        "target": 137
      }, {
        "source": 188,
        "target": 137
      }, {
        "source": 188,
        "target": 147
      }, {
        "source": 189,
        "target": 149
      }, {
        "source": 189,
        "target": 188
      }, {
        "source": 189,
        "target": 7
      }, {
        "source": 190,
        "target": 137
      }, {
        "source": 190,
        "target": 147
      }, {
        "source": 190,
        "target": 108
      }, {
        "source": 190,
        "target": 95
      }, {
        "source": 190,
        "target": 148
      }, {
        "source": 191,
        "target": 137
      }, {
        "source": 192,
        "target": 137
      }, {
        "source": 193,
        "target": 137
      }, {
        "source": 194,
        "target": 137
      }, {
        "source": 194,
        "target": 95
      }, {
        "source": 195,
        "target": 137
      }, {
        "source": 196,
        "target": 137
      }, {
        "source": 197,
        "target": 137
      }, {
        "source": 197,
        "target": 147
      }, {
        "source": 197,
        "target": 95
      }, {
        "source": 197,
        "target": 189
      }, {
        "source": 197,
        "target": 149
      }, {
        "source": 197,
        "target": 148
      }, {
        "source": 197,
        "target": 7
      }, {
        "source": 198,
        "target": 137
      }, {
        "source": 199,
        "target": 137
      }, {
        "source": 200,
        "target": 137
      }, {
        "source": 201,
        "target": 137
      }, {
        "source": 201,
        "target": 70
      }, {
        "source": 202,
        "target": 137
      }, {
        "source": 203,
        "target": 137
      }, {
        "source": 204,
        "target": 137
      }, {
        "source": 205,
        "target": 137
      }, {
        "source": 205,
        "target": 202
      }, {
        "source": 205,
        "target": 149
      }, {
        "source": 205,
        "target": 206
      }, {
        "source": 207,
        "target": 137
      }, {
        "source": 207,
        "target": 149
      }, {
        "source": 206,
        "target": 137
      }, {
        "source": 206,
        "target": 149
      }, {
        "source": 208,
        "target": 137
      }, {
        "source": 208,
        "target": 147
      }, {
        "source": 208,
        "target": 95
      }, {
        "source": 208,
        "target": 148
      }, {
        "source": 209,
        "target": 137
      }, {
        "source": 210,
        "target": 137
      }, {
        "source": 210,
        "target": 180
      }, {
        "source": 211,
        "target": 137
      }, {
        "source": 212,
        "target": 137
      }, {
        "source": 40,
        "target": 141
      }, {
        "source": 213,
        "target": 214
      }, {
        "source": 213,
        "target": 215
      }, {
        "source": 213,
        "target": 216
      }, {
        "source": 217,
        "target": 213
      }, {
        "source": 218,
        "target": 219
      }, {
        "source": 218,
        "target": 214
      }, {
        "source": 218,
        "target": 220
      }, {
        "source": 218,
        "target": 221
      }, {
        "source": 222,
        "target": 215
      }, {
        "source": 222,
        "target": 223
      }, {
        "source": 222,
        "target": 224
      }, {
        "source": 222,
        "target": 216
      }, {
        "source": 225,
        "target": 214
      }, {
        "source": 225,
        "target": 220
      }, {
        "source": 225,
        "target": 216
      }, {
        "source": 226,
        "target": 215
      }, {
        "source": 226,
        "target": 226
      }, {
        "source": 220,
        "target": 219
      }, {
        "source": 220,
        "target": 214
      }, {
        "source": 220,
        "target": 221
      }, {
        "source": 220,
        "target": 216
      }, {
        "source": 220,
        "target": 225
      }, {
        "source": 224,
        "target": 216
      }, {
        "source": 216,
        "target": 227
      }, {
        "source": 216,
        "target": 214
      }, {
        "source": 216,
        "target": 221
      }, {
        "source": 221,
        "target": 218
      }, {
        "source": 221,
        "target": 227
      }, {
        "source": 221,
        "target": 220
      }, {
        "source": 223,
        "target": 216
      }, {
        "source": 228,
        "target": 5
      }, {
        "source": 228,
        "target": 228
      }, {
        "source": 229,
        "target": 5
      }, {
        "source": 229,
        "target": 229
      }, {
        "source": 230,
        "target": 5
      }, {
        "source": 230,
        "target": 230
      }, {
        "source": 231,
        "target": 231
      }, {
        "source": 232,
        "target": 233
      }, {
        "source": 234,
        "target": 219
      }, {
        "source": 177,
        "target": 176
      }, {
        "source": 25,
        "target": 12
      }, {
        "source": 25,
        "target": 141
      }, {
        "source": 235,
        "target": 236
      }, {
        "source": 236,
        "target": 235
      }, {
        "source": 237,
        "target": 238
      }, {
        "source": 237,
        "target": 239
      }, {
        "source": 233,
        "target": 240
      }, {
        "source": 26,
        "target": 12
      }, {
        "source": 26,
        "target": 233
      }, {
        "source": 27,
        "target": 12
      }, {
        "source": 27,
        "target": 233
      }, {
        "source": 241,
        "target": 233
      }, {
        "source": 240,
        "target": 242
      }, {
        "source": 243,
        "target": 244
      }, {
        "source": 115,
        "target": 117
      }, {
        "source": 245,
        "target": 7
      }, {
        "source": 246,
        "target": 95
      }, {
        "source": 246,
        "target": 7
      }, {
        "source": 97,
        "target": 7
      }, {
        "source": 247,
        "target": 131
      }, {
        "source": 247,
        "target": 104
      }, {
        "source": 247,
        "target": 105
      }, {
        "source": 247,
        "target": 248
      }, {
        "source": 247,
        "target": 129
      }, {
        "source": 249,
        "target": 250
      }, {
        "source": 251,
        "target": 232
      }, {
        "source": 7,
        "target": 97
      }, {
        "source": 7,
        "target": 95
      }, {
        "source": 7,
        "target": 7
      }, {
        "source": 7,
        "target": 8
      }, {
        "source": 252,
        "target": 7
      }, {
        "source": 253,
        "target": 252
      }, {
        "source": 253,
        "target": 7
      }, {
        "source": 95,
        "target": 7
      }, {
        "source": 254,
        "target": 7
      }, {
        "source": 255,
        "target": 256
      }, {
        "source": 257,
        "target": 255
      }, {
        "source": 257,
        "target": 87
      }, {
        "source": 258,
        "target": 259
      }, {
        "source": 28,
        "target": 12
      }, {
        "source": 28,
        "target": 14
      }, {
        "source": 28,
        "target": 32
      }, {
        "source": 29,
        "target": 1
      }, {
        "source": 260,
        "target": 52
      }, {
        "source": 260,
        "target": 261
      }, {
        "source": 260,
        "target": 262
      }, {
        "source": 132,
        "target": 133
      }, {
        "source": 263,
        "target": 264
      }, {
        "source": 265,
        "target": 7
      }, {
        "source": 265,
        "target": 70
      }, {
        "source": 266,
        "target": 95
      }, {
        "source": 107,
        "target": 7
      }, {
        "source": 107,
        "target": 94
      }, {
        "source": 107,
        "target": 107
      }, {
        "source": 107,
        "target": 46
      }, {
        "source": 107,
        "target": 45
      }, {
        "source": 68,
        "target": 64
      }, {
        "source": 67,
        "target": 64
      }, {
        "source": 267,
        "target": 4
      }, {
        "source": 267,
        "target": 5
      }, {
        "source": 268,
        "target": 269
      }, {
        "source": 268,
        "target": 241
      }, {
        "source": 268,
        "target": 270
      }, {
        "source": 268,
        "target": 233
      }, {
        "source": 268,
        "target": 271
      }, {
        "source": 268,
        "target": 267
      }, {
        "source": 268,
        "target": 272
      }, {
        "source": 271,
        "target": 269
      }, {
        "source": 272,
        "target": 273
      }, {
        "source": 274,
        "target": 275
      }, {
        "source": 30,
        "target": 1
      }, {
        "source": 276,
        "target": 277
      }, {
        "source": 111,
        "target": 94
      }, {
        "source": 111,
        "target": 8
      }, {
        "source": 111,
        "target": 7
      }, {
        "source": 111,
        "target": 95
      }, {
        "source": 111,
        "target": 106
      }, {
        "source": 278,
        "target": 279
      }, {
        "source": 278,
        "target": 244
      }, {
        "source": 280,
        "target": 84
      }, {
        "source": 239,
        "target": 176
      }, {
        "source": 239,
        "target": 2
      }, {
        "source": 238,
        "target": 239
      }, {
        "source": 281,
        "target": 282
      }, {
        "source": 283,
        "target": 284
      }, {
        "source": 285,
        "target": 281
      }, {
        "source": 286,
        "target": 287
      }, {
        "source": 288,
        "target": 286
      }, {
        "source": 289,
        "target": 290
      }, {
        "source": 291,
        "target": 292
      }, {
        "source": 293,
        "target": 292
      }, {
        "source": 74,
        "target": 292
      }, {
        "source": 294,
        "target": 295
      }, {
        "source": 296,
        "target": 289
      }, {
        "source": 77,
        "target": 296
      }, {
        "source": 297,
        "target": 298
      }, {
        "source": 297,
        "target": 299
      }, {
        "source": 300,
        "target": 301
      }, {
        "source": 70,
        "target": 59
      }, {
        "source": 70,
        "target": 7
      }, {
        "source": 70,
        "target": 70
      }, {
        "source": 302,
        "target": 70
      }, {
        "source": 303,
        "target": 304
      }, {
        "source": 303,
        "target": 305
      }, {
        "source": 306,
        "target": 307
      }, {
        "source": 308,
        "target": 309
      }, {
        "source": 310,
        "target": 307
      }, {
        "source": 311,
        "target": 312
      }, {
        "source": 313,
        "target": 314
      }, {
        "source": 315,
        "target": 316
      }, {
        "source": 317,
        "target": 318
      }, {
        "source": 319,
        "target": 320
      }, {
        "source": 321,
        "target": 322
      }, {
        "source": 323,
        "target": 324
      }, {
        "source": 325,
        "target": 326
      }, {
        "source": 327,
        "target": 312
      }, {
        "source": 328,
        "target": 312
      }, {
        "source": 329,
        "target": 312
      }, {
        "source": 312,
        "target": 330
      }, {
        "source": 312,
        "target": 307
      }, {
        "source": 331,
        "target": 304
      }, {
        "source": 331,
        "target": 315
      }, {
        "source": 332,
        "target": 304
      }, {
        "source": 332,
        "target": 333
      }, {
        "source": 334,
        "target": 65
      }, {
        "source": 334,
        "target": 67
      }, {
        "source": 335,
        "target": 307
      }, {
        "source": 335,
        "target": 336
      }, {
        "source": 335,
        "target": 319
      }, {
        "source": 335,
        "target": 333
      }, {
        "source": 337,
        "target": 338
      }, {
        "source": 337,
        "target": 315
      }, {
        "source": 339,
        "target": 304
      }, {
        "source": 340,
        "target": 341
      }, {
        "source": 157,
        "target": 342
      }, {
        "source": 307,
        "target": 8
      }, {
        "source": 307,
        "target": 342
      }, {
        "source": 307,
        "target": 307
      }, {
        "source": 343,
        "target": 344
      }, {
        "source": 343,
        "target": 345
      }, {
        "source": 343,
        "target": 307
      }, {
        "source": 343,
        "target": 346
      }, {
        "source": 343,
        "target": 343
      }, {
        "source": 345,
        "target": 343
      }, {
        "source": 347,
        "target": 304
      }, {
        "source": 347,
        "target": 315
      }, {
        "source": 338,
        "target": 348
      }, {
        "source": 349,
        "target": 350
      }, {
        "source": 349,
        "target": 305
      }, {
        "source": 349,
        "target": 333
      }, {
        "source": 351,
        "target": 350
      }, {
        "source": 351,
        "target": 305
      }, {
        "source": 351,
        "target": 333
      }, {
        "source": 351,
        "target": 319
      }, {
        "source": 352,
        "target": 350
      }, {
        "source": 352,
        "target": 305
      }, {
        "source": 353,
        "target": 350
      }, {
        "source": 353,
        "target": 305
      }, {
        "source": 353,
        "target": 336
      }, {
        "source": 353,
        "target": 333
      }, {
        "source": 354,
        "target": 350
      }, {
        "source": 354,
        "target": 336
      }, {
        "source": 354,
        "target": 333
      }, {
        "source": 354,
        "target": 305
      }, {
        "source": 354,
        "target": 319
      }, {
        "source": 354,
        "target": 355
      }, {
        "source": 354,
        "target": 348
      }, {
        "source": 356,
        "target": 350
      }, {
        "source": 356,
        "target": 336
      }, {
        "source": 356,
        "target": 305
      }, {
        "source": 357,
        "target": 350
      }, {
        "source": 357,
        "target": 305
      }, {
        "source": 357,
        "target": 336
      }, {
        "source": 357,
        "target": 333
      }, {
        "source": 358,
        "target": 307
      }, {
        "source": 358,
        "target": 336
      }, {
        "source": 359,
        "target": 350
      }, {
        "source": 359,
        "target": 336
      }, {
        "source": 359,
        "target": 305
      }, {
        "source": 360,
        "target": 350
      }, {
        "source": 361,
        "target": 335
      }, {
        "source": 362,
        "target": 335
      }, {
        "source": 363,
        "target": 335
      }, {
        "source": 364,
        "target": 335
      }, {
        "source": 365,
        "target": 350
      }, {
        "source": 365,
        "target": 305
      }, {
        "source": 365,
        "target": 336
      }, {
        "source": 366,
        "target": 350
      }, {
        "source": 366,
        "target": 321
      }, {
        "source": 367,
        "target": 350
      }, {
        "source": 368,
        "target": 307
      }, {
        "source": 368,
        "target": 305
      }, {
        "source": 369,
        "target": 350
      }, {
        "source": 369,
        "target": 305
      }, {
        "source": 369,
        "target": 333
      }, {
        "source": 369,
        "target": 336
      }, {
        "source": 370,
        "target": 350
      }, {
        "source": 370,
        "target": 336
      }, {
        "source": 370,
        "target": 305
      }, {
        "source": 371,
        "target": 307
      }, {
        "source": 371,
        "target": 336
      }, {
        "source": 372,
        "target": 350
      }, {
        "source": 372,
        "target": 305
      }, {
        "source": 372,
        "target": 336
      }, {
        "source": 373,
        "target": 307
      }, {
        "source": 373,
        "target": 336
      }, {
        "source": 374,
        "target": 350
      }, {
        "source": 374,
        "target": 305
      }, {
        "source": 375,
        "target": 350
      }, {
        "source": 375,
        "target": 336
      }, {
        "source": 375,
        "target": 355
      }, {
        "source": 375,
        "target": 333
      }, {
        "source": 376,
        "target": 341
      }, {
        "source": 376,
        "target": 355
      }, {
        "source": 376,
        "target": 333
      }, {
        "source": 376,
        "target": 315
      }, {
        "source": 350,
        "target": 341
      }, {
        "source": 350,
        "target": 315
      }, {
        "source": 350,
        "target": 305
      }, {
        "source": 377,
        "target": 321
      }, {
        "source": 377,
        "target": 323
      }, {
        "source": 378,
        "target": 307
      }, {
        "source": 379,
        "target": 307
      }, {
        "source": 380,
        "target": 307
      }, {
        "source": 381,
        "target": 307
      }, {
        "source": 382,
        "target": 307
      }, {
        "source": 383,
        "target": 307
      }, {
        "source": 384,
        "target": 304
      }, {
        "source": 384,
        "target": 315
      }, {
        "source": 385,
        "target": 304
      }, {
        "source": 386,
        "target": 307
      }, {
        "source": 387,
        "target": 341
      }, {
        "source": 388,
        "target": 341
      }, {
        "source": 388,
        "target": 325
      }, {
        "source": 388,
        "target": 333
      }, {
        "source": 389,
        "target": 307
      }, {
        "source": 390,
        "target": 304
      }, {
        "source": 390,
        "target": 315
      }, {
        "source": 390,
        "target": 321
      }, {
        "source": 318,
        "target": 316
      }, {
        "source": 391,
        "target": 388
      }, {
        "source": 391,
        "target": 315
      }, {
        "source": 392,
        "target": 304
      }, {
        "source": 392,
        "target": 315
      }, {
        "source": 393,
        "target": 307
      }, {
        "source": 393,
        "target": 324
      }, {
        "source": 393,
        "target": 394
      }, {
        "source": 395,
        "target": 377
      }, {
        "source": 395,
        "target": 315
      }, {
        "source": 395,
        "target": 333
      }, {
        "source": 395,
        "target": 313
      }, {
        "source": 395,
        "target": 314
      }, {
        "source": 396,
        "target": 341
      }, {
        "source": 396,
        "target": 315
      }, {
        "source": 396,
        "target": 333
      }, {
        "source": 394,
        "target": 394
      }, {
        "source": 397,
        "target": 307
      }, {
        "source": 398,
        "target": 307
      }, {
        "source": 399,
        "target": 338
      }, {
        "source": 320,
        "target": 400
      }, {
        "source": 401,
        "target": 334
      }, {
        "source": 402,
        "target": 304
      }, {
        "source": 402,
        "target": 403
      }, {
        "source": 402,
        "target": 336
      }, {
        "source": 402,
        "target": 404
      }, {
        "source": 402,
        "target": 405
      }, {
        "source": 402,
        "target": 406
      }, {
        "source": 402,
        "target": 407
      }, {
        "source": 402,
        "target": 408
      }, {
        "source": 402,
        "target": 409
      }, {
        "source": 402,
        "target": 410
      }, {
        "source": 402,
        "target": 411
      }, {
        "source": 402,
        "target": 412
      }, {
        "source": 402,
        "target": 413
      }, {
        "source": 402,
        "target": 414
      }, {
        "source": 402,
        "target": 415
      }, {
        "source": 402,
        "target": 416
      }, {
        "source": 402,
        "target": 417
      }, {
        "source": 402,
        "target": 418
      }, {
        "source": 402,
        "target": 419
      }, {
        "source": 402,
        "target": 420
      }, {
        "source": 402,
        "target": 421
      }, {
        "source": 402,
        "target": 422
      }, {
        "source": 402,
        "target": 423
      }, {
        "source": 404,
        "target": 424
      }, {
        "source": 405,
        "target": 424
      }, {
        "source": 406,
        "target": 424
      }, {
        "source": 407,
        "target": 424
      }, {
        "source": 408,
        "target": 424
      }, {
        "source": 409,
        "target": 424
      }, {
        "source": 410,
        "target": 424
      }, {
        "source": 411,
        "target": 424
      }, {
        "source": 412,
        "target": 424
      }, {
        "source": 413,
        "target": 424
      }, {
        "source": 414,
        "target": 424
      }, {
        "source": 415,
        "target": 424
      }, {
        "source": 416,
        "target": 424
      }, {
        "source": 417,
        "target": 424
      }, {
        "source": 418,
        "target": 424
      }, {
        "source": 419,
        "target": 424
      }, {
        "source": 420,
        "target": 424
      }, {
        "source": 403,
        "target": 424
      }, {
        "source": 421,
        "target": 424
      }, {
        "source": 422,
        "target": 424
      }, {
        "source": 425,
        "target": 377
      }, {
        "source": 425,
        "target": 315
      }, {
        "source": 425,
        "target": 333
      }, {
        "source": 425,
        "target": 325
      }, {
        "source": 423,
        "target": 423
      }, {
        "source": 426,
        "target": 423
      }, {
        "source": 427,
        "target": 304
      }, {
        "source": 427,
        "target": 426
      }, {
        "source": 428,
        "target": 304
      }, {
        "source": 428,
        "target": 426
      }, {
        "source": 429,
        "target": 388
      }, {
        "source": 429,
        "target": 315
      }, {
        "source": 430,
        "target": 304
      }, {
        "source": 430,
        "target": 315
      }, {
        "source": 431,
        "target": 338
      }, {
        "source": 432,
        "target": 312
      }, {
        "source": 433,
        "target": 341
      }, {
        "source": 433,
        "target": 336
      }, {
        "source": 341,
        "target": 305
      }, {
        "source": 341,
        "target": 57
      }, {
        "source": 341,
        "target": 65
      }, {
        "source": 434,
        "target": 435
      }, {
        "source": 342,
        "target": 436
      }, {
        "source": 342,
        "target": 423
      }, {
        "source": 342,
        "target": 437
      }, {
        "source": 342,
        "target": 315
      }, {
        "source": 342,
        "target": 324
      }, {
        "source": 342,
        "target": 307
      }, {
        "source": 342,
        "target": 314
      }, {
        "source": 342,
        "target": 316
      }, {
        "source": 342,
        "target": 394
      }, {
        "source": 342,
        "target": 400
      }, {
        "source": 342,
        "target": 438
      }, {
        "source": 342,
        "target": 8
      }, {
        "source": 342,
        "target": 95
      }, {
        "source": 439,
        "target": 304
      }, {
        "source": 440,
        "target": 377
      }, {
        "source": 441,
        "target": 442
      }, {
        "source": 443,
        "target": 341
      }, {
        "source": 443,
        "target": 333
      }, {
        "source": 443,
        "target": 315
      }, {
        "source": 443,
        "target": 423
      }, {
        "source": 443,
        "target": 324
      }, {
        "source": 444,
        "target": 304
      }, {
        "source": 445,
        "target": 309
      }, {
        "source": 445,
        "target": 333
      }, {
        "source": 445,
        "target": 315
      }, {
        "source": 446,
        "target": 443
      }, {
        "source": 446,
        "target": 317
      }, {
        "source": 446,
        "target": 319
      }, {
        "source": 447,
        "target": 341
      }, {
        "source": 438,
        "target": 394
      }, {
        "source": 304,
        "target": 393
      }, {
        "source": 304,
        "target": 325
      }, {
        "source": 326,
        "target": 438
      }, {
        "source": 448,
        "target": 309
      }, {
        "source": 449,
        "target": 446
      }, {
        "source": 309,
        "target": 305
      }, {
        "source": 346,
        "target": 304
      }, {
        "source": 346,
        "target": 343
      }, {
        "source": 346,
        "target": 315
      }, {
        "source": 450,
        "target": 436
      }, {
        "source": 450,
        "target": 442
      }, {
        "source": 437,
        "target": 321
      }, {
        "source": 437,
        "target": 326
      }, {
        "source": 437,
        "target": 323
      }, {
        "source": 437,
        "target": 307
      }, {
        "source": 451,
        "target": 307
      }, {
        "source": 43,
        "target": 44
      }, {
        "source": 43,
        "target": 43
      }, {
        "source": 180,
        "target": 452
      }, {
        "source": 180,
        "target": 453
      }, {
        "source": 453,
        "target": 180
      }, {
        "source": 453,
        "target": 94
      }, {
        "source": 452,
        "target": 453
      }, {
        "source": 179,
        "target": 180
      }, {
        "source": 454,
        "target": 344
      }, {
        "source": 455,
        "target": 454
      }, {
        "source": 456,
        "target": 7
      }, {
        "source": 456,
        "target": 252
      }, {
        "source": 457,
        "target": 5
      }, {
        "source": 457,
        "target": 457
      }, {
        "source": 458,
        "target": 5
      }, {
        "source": 458,
        "target": 458
      }, {
        "source": 2,
        "target": 5
      }, {
        "source": 2,
        "target": 2
      }, {
        "source": 459,
        "target": 2
      }, {
        "source": 459,
        "target": 459
      }, {
        "source": 31,
        "target": 1
      }, {
        "source": 31,
        "target": 3
      }, {
        "source": 460,
        "target": 33
      }, {
        "source": 460,
        "target": 461
      }, {
        "source": 460,
        "target": 462
      }, {
        "source": 460,
        "target": 463
      }, {
        "source": 460,
        "target": 464
      }, {
        "source": 460,
        "target": 465
      }, {
        "source": 460,
        "target": 4
      }, {
        "source": 460,
        "target": 5
      }, {
        "source": 460,
        "target": 466
      }, {
        "source": 460,
        "target": 467
      }, {
        "source": 460,
        "target": 468
      }, {
        "source": 460,
        "target": 469
      }, {
        "source": 460,
        "target": 470
      }, {
        "source": 460,
        "target": 36
      }, {
        "source": 460,
        "target": 39
      }, {
        "source": 460,
        "target": 34
      }, {
        "source": 460,
        "target": 40
      }, {
        "source": 460,
        "target": 3
      }, {
        "source": 471,
        "target": 472
      }, {
        "source": 473,
        "target": 72
      }, {
        "source": 474,
        "target": 55
      }, {
        "source": 474,
        "target": 57
      }, {
        "source": 475,
        "target": 55
      }, {
        "source": 475,
        "target": 62
      }, {
        "source": 475,
        "target": 474
      }, {
        "source": 476,
        "target": 476
      }, {
        "source": 477,
        "target": 72
      }, {
        "source": 478,
        "target": 72
      }, {
        "source": 479,
        "target": 95
      }, {
        "source": 480,
        "target": 4
      }, {
        "source": 480,
        "target": 5
      }, {
        "source": 481,
        "target": 279
      }, {
        "source": 84,
        "target": 222
      }, {
        "source": 84,
        "target": 482
      }, {
        "source": 84,
        "target": 483
      }, {
        "source": 84,
        "target": 84
      }, {
        "source": 84,
        "target": 257
      }, {
        "source": 84,
        "target": 73
      }, {
        "source": 84,
        "target": 76
      }, {
        "source": 84,
        "target": 126
      }, {
        "source": 84,
        "target": 99
      }, {
        "source": 84,
        "target": 89
      }, {
        "source": 484,
        "target": 485
      }, {
        "source": 484,
        "target": 4
      }, {
        "source": 484,
        "target": 5
      }, {
        "source": 484,
        "target": 486
      }, {
        "source": 487,
        "target": 488
      }, {
        "source": 487,
        "target": 489
      }, {
        "source": 487,
        "target": 490
      }, {
        "source": 488,
        "target": 490
      }, {
        "source": 490,
        "target": 7
      }, {
        "source": 491,
        "target": 7
      }, {
        "source": 491,
        "target": 94
      }]
    };
    var option = {
      legend: {
        data: ['HTMLElement', 'WebGL', 'SVG', 'CSS', 'Other']
      },
      series: [{
        type: 'graph',
        layout: 'force',
        animation: false,
        label: {
          normal: {
            position: 'right',
            formatter: '{b}'
          }
        },
        draggable: true,
        data: webkitDep.nodes.map(function (node, idx) {
          node.id = idx;
          return node;
        }),
        categories: webkitDep.categories,
        force: {
          // initLayout: 'circular'
          // repulsion: 20,
          edgeLength: 5,
          repulsion: 20,
          gravity: 0.2
        },
        edges: webkitDep.links
      }]
    };
    return /*#__PURE__*/_react["default"].createElement(_echartsForReact["default"], {
      option: option,
      style: {
        height: '700px',
        width: '100%'
      }
    });
  };

  var _default;

  return _react["default"].createElement(Page);
},
    previewerProps: {"sources":{"_":{"tsx":"import React from 'react';\nimport ReactECharts from 'echarts-for-react';\n\nconst Page: React.FC = () => {\n  const webkitDep = {\"type\":\"force\",\"categories\":[{\"name\":\"HTMLElement\",\"keyword\":{},\"base\":\"HTMLElement\"},{\"name\":\"WebGL\",\"keyword\":{},\"base\":\"WebGLRenderingContext\"},{\"name\":\"SVG\",\"keyword\":{},\"base\":\"SVGElement\"},{\"name\":\"CSS\",\"keyword\":{},\"base\":\"CSSRule\"},{\"name\":\"Other\",\"keyword\":{}}],\"nodes\":[{\"name\":\"AnalyserNode\",\"value\":1,\"category\":4},{\"name\":\"AudioNode\",\"value\":1,\"category\":4},{\"name\":\"Uint8Array\",\"value\":1,\"category\":4},{\"name\":\"Float32Array\",\"value\":1,\"category\":4},{\"name\":\"ArrayBuffer\",\"value\":1,\"category\":4},{\"name\":\"ArrayBufferView\",\"value\":1,\"category\":4},{\"name\":\"Attr\",\"value\":1,\"category\":4},{\"name\":\"Node\",\"value\":1,\"category\":4},{\"name\":\"Element\",\"value\":1,\"category\":4},{\"name\":\"AudioBuffer\",\"value\":1,\"category\":4},{\"name\":\"AudioBufferCallback\",\"value\":1,\"category\":4},{\"name\":\"AudioBufferSourceNode\",\"value\":1,\"category\":4},{\"name\":\"AudioSourceNode\",\"value\":1,\"category\":4},{\"name\":\"AudioGain\",\"value\":1,\"category\":4},{\"name\":\"AudioParam\",\"value\":1,\"category\":4},{\"name\":\"AudioContext\",\"value\":1,\"category\":4},{\"name\":\"AudioDestinationNode\",\"value\":1,\"category\":4},{\"name\":\"AudioListener\",\"value\":1,\"category\":4},{\"name\":\"BiquadFilterNode\",\"value\":1,\"category\":4},{\"name\":\"ChannelMergerNode\",\"value\":1,\"category\":4},{\"name\":\"ChannelSplitterNode\",\"value\":1,\"category\":4},{\"name\":\"ConvolverNode\",\"value\":1,\"category\":4},{\"name\":\"DelayNode\",\"value\":1,\"category\":4},{\"name\":\"DynamicsCompressorNode\",\"value\":1,\"category\":4},{\"name\":\"GainNode\",\"value\":1,\"category\":4},{\"name\":\"MediaElementAudioSourceNode\",\"value\":1,\"category\":4},{\"name\":\"MediaStreamAudioDestinationNode\",\"value\":1,\"category\":4},{\"name\":\"MediaStreamAudioSourceNode\",\"value\":1,\"category\":4},{\"name\":\"OscillatorNode\",\"value\":1,\"category\":4},{\"name\":\"PannerNode\",\"value\":1,\"category\":4},{\"name\":\"ScriptProcessorNode\",\"value\":1,\"category\":4},{\"name\":\"WaveShaperNode\",\"value\":1,\"category\":4},{\"name\":\"WaveTable\",\"value\":1,\"category\":4},{\"name\":\"CanvasRenderingContext\",\"value\":1,\"category\":4},{\"name\":\"HTMLCanvasElement\",\"value\":1,\"category\":0},{\"name\":\"CanvasRenderingContext2D\",\"value\":1,\"category\":4},{\"name\":\"ImageData\",\"value\":1,\"category\":4},{\"name\":\"CanvasGradient\",\"value\":1,\"category\":4},{\"name\":\"CanvasPattern\",\"value\":1,\"category\":4},{\"name\":\"HTMLImageElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLVideoElement\",\"value\":1,\"category\":0},{\"name\":\"TextMetrics\",\"value\":1,\"category\":4},{\"name\":\"CDATASection\",\"value\":1,\"category\":4},{\"name\":\"Text\",\"value\":1,\"category\":4},{\"name\":\"CharacterData\",\"value\":1,\"category\":4},{\"name\":\"ClientRectList\",\"value\":1,\"category\":4},{\"name\":\"ClientRect\",\"value\":1,\"category\":4},{\"name\":\"Clipboard\",\"value\":1,\"category\":4},{\"name\":\"FileList\",\"value\":1,\"category\":4},{\"name\":\"DataTransferItemList\",\"value\":1,\"category\":4},{\"name\":\"Comment\",\"value\":1,\"category\":4},{\"name\":\"Console\",\"value\":1,\"category\":4},{\"name\":\"MemoryInfo\",\"value\":1,\"category\":4},{\"name\":\"Crypto\",\"value\":1,\"category\":4},{\"name\":\"CSSCharsetRule\",\"value\":1,\"category\":3},{\"name\":\"CSSRule\",\"value\":3,\"category\":3},{\"name\":\"CSSFontFaceRule\",\"value\":1,\"category\":3},{\"name\":\"CSSStyleDeclaration\",\"value\":1,\"category\":3},{\"name\":\"CSSImportRule\",\"value\":1,\"category\":3},{\"name\":\"MediaList\",\"value\":1,\"category\":4},{\"name\":\"CSSStyleSheet\",\"value\":1,\"category\":3},{\"name\":\"CSSMediaRule\",\"value\":1,\"category\":3},{\"name\":\"CSSRuleList\",\"value\":1,\"category\":3},{\"name\":\"CSSPageRule\",\"value\":1,\"category\":3},{\"name\":\"CSSPrimitiveValue\",\"value\":1,\"category\":3},{\"name\":\"CSSValue\",\"value\":1,\"category\":3},{\"name\":\"Counter\",\"value\":1,\"category\":4},{\"name\":\"RGBColor\",\"value\":1,\"category\":4},{\"name\":\"Rect\",\"value\":1,\"category\":4},{\"name\":\"CSSStyleRule\",\"value\":1,\"category\":3},{\"name\":\"StyleSheet\",\"value\":1,\"category\":4},{\"name\":\"CSSUnknownRule\",\"value\":1,\"category\":3},{\"name\":\"CSSValueList\",\"value\":1,\"category\":3},{\"name\":\"Database\",\"value\":1,\"category\":4},{\"name\":\"SQLTransactionCallback\",\"value\":1,\"category\":4},{\"name\":\"DatabaseCallback\",\"value\":1,\"category\":4},{\"name\":\"DatabaseSync\",\"value\":1,\"category\":4},{\"name\":\"SQLTransactionSyncCallback\",\"value\":1,\"category\":4},{\"name\":\"DataTransferItem\",\"value\":1,\"category\":4},{\"name\":\"StringCallback\",\"value\":1,\"category\":4},{\"name\":\"Entry\",\"value\":1,\"category\":4},{\"name\":\"File\",\"value\":1,\"category\":4},{\"name\":\"DataView\",\"value\":1,\"category\":4},{\"name\":\"DedicatedWorkerContext\",\"value\":1,\"category\":4},{\"name\":\"WorkerContext\",\"value\":1,\"category\":4},{\"name\":\"DirectoryEntry\",\"value\":1,\"category\":4},{\"name\":\"DirectoryReader\",\"value\":1,\"category\":4},{\"name\":\"VoidCallback\",\"value\":1,\"category\":4},{\"name\":\"DirectoryEntrySync\",\"value\":1,\"category\":4},{\"name\":\"EntrySync\",\"value\":1,\"category\":4},{\"name\":\"DirectoryReaderSync\",\"value\":1,\"category\":4},{\"name\":\"FileEntrySync\",\"value\":1,\"category\":4},{\"name\":\"EntriesCallback\",\"value\":1,\"category\":4},{\"name\":\"EntryArraySync\",\"value\":1,\"category\":4},{\"name\":\"DocumentFragment\",\"value\":1,\"category\":4},{\"name\":\"NodeList\",\"value\":1,\"category\":4},{\"name\":\"DocumentType\",\"value\":1,\"category\":4},{\"name\":\"NamedNodeMap\",\"value\":1,\"category\":4},{\"name\":\"DOMFileSystem\",\"value\":1,\"category\":4},{\"name\":\"DOMFileSystemSync\",\"value\":1,\"category\":4},{\"name\":\"DOMImplementation\",\"value\":1,\"category\":4},{\"name\":\"HTMLDocument\",\"value\":1,\"category\":0},{\"name\":\"DOMMimeType\",\"value\":1,\"category\":4},{\"name\":\"DOMPlugin\",\"value\":1,\"category\":4},{\"name\":\"DOMMimeTypeArray\",\"value\":1,\"category\":4},{\"name\":\"DOMPluginArray\",\"value\":1,\"category\":4},{\"name\":\"DOMSelection\",\"value\":1,\"category\":4},{\"name\":\"Range\",\"value\":1,\"category\":4},{\"name\":\"DOMSettableTokenList\",\"value\":1,\"category\":4},{\"name\":\"DOMTokenList\",\"value\":1,\"category\":4},{\"name\":\"DOMStringMap\",\"value\":1,\"category\":4},{\"name\":\"ShadowRoot\",\"value\":1,\"category\":4},{\"name\":\"Entity\",\"value\":1,\"category\":4},{\"name\":\"EntityReference\",\"value\":1,\"category\":4},{\"name\":\"EntryArray\",\"value\":1,\"category\":4},{\"name\":\"MetadataCallback\",\"value\":1,\"category\":4},{\"name\":\"EntryCallback\",\"value\":1,\"category\":4},{\"name\":\"Metadata\",\"value\":1,\"category\":4},{\"name\":\"ErrorCallback\",\"value\":1,\"category\":4},{\"name\":\"FileError\",\"value\":1,\"category\":4},{\"name\":\"FileCallback\",\"value\":1,\"category\":4},{\"name\":\"FileEntry\",\"value\":1,\"category\":4},{\"name\":\"FileWriterCallback\",\"value\":1,\"category\":4},{\"name\":\"FileWriterSync\",\"value\":1,\"category\":4},{\"name\":\"FileReader\",\"value\":1,\"category\":4},{\"name\":\"FileReaderSync\",\"value\":1,\"category\":4},{\"name\":\"FileSystemCallback\",\"value\":1,\"category\":4},{\"name\":\"FileWriter\",\"value\":1,\"category\":4},{\"name\":\"Float64Array\",\"value\":1,\"category\":4},{\"name\":\"GamepadList\",\"value\":1,\"category\":4},{\"name\":\"Gamepad\",\"value\":1,\"category\":4},{\"name\":\"Geolocation\",\"value\":1,\"category\":4},{\"name\":\"PositionCallback\",\"value\":1,\"category\":4},{\"name\":\"Geoposition\",\"value\":1,\"category\":4},{\"name\":\"Coordinates\",\"value\":1,\"category\":4},{\"name\":\"HTMLAllCollection\",\"value\":1,\"category\":0},{\"name\":\"HTMLAnchorElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLElement\",\"value\":3,\"category\":0},{\"name\":\"HTMLAppletElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLAreaElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLAudioElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLMediaElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLBaseElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLBaseFontElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLBodyElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLBRElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLButtonElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLFormElement\",\"value\":1,\"category\":0},{\"name\":\"ValidityState\",\"value\":1,\"category\":4},{\"name\":\"HTMLCollection\",\"value\":1,\"category\":0},{\"name\":\"HTMLContentElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLDataListElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLDetailsElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLDirectoryElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLDivElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLDListElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLEmbedElement\",\"value\":1,\"category\":0},{\"name\":\"SVGDocument\",\"value\":1,\"category\":2},{\"name\":\"HTMLFieldSetElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLFontElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLFormControlsCollection\",\"value\":1,\"category\":0},{\"name\":\"HTMLFrameElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLFrameSetElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLHeadElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLHeadingElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLHRElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLHtmlElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLIFrameElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLInputElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLKeygenElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLLabelElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLLegendElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLLIElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLLinkElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLMapElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLMarqueeElement\",\"value\":1,\"category\":0},{\"name\":\"TimeRanges\",\"value\":1,\"category\":4},{\"name\":\"MediaController\",\"value\":1,\"category\":4},{\"name\":\"MediaError\",\"value\":1,\"category\":4},{\"name\":\"TextTrackList\",\"value\":1,\"category\":4},{\"name\":\"TextTrack\",\"value\":1,\"category\":4},{\"name\":\"HTMLMenuElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLMetaElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLMeterElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLModElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLObjectElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLOListElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLOptGroupElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLOptionElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLOptionsCollection\",\"value\":1,\"category\":0},{\"name\":\"HTMLOutputElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLParagraphElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLParamElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLPreElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLProgressElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLQuoteElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLScriptElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLSelectElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLShadowElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLSourceElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLSpanElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLStyleElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLTableCaptionElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLTableCellElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLTableColElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLTableElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLTableSectionElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLTableRowElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLTextAreaElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLTitleElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLTrackElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLUListElement\",\"value\":1,\"category\":0},{\"name\":\"HTMLUnknownElement\",\"value\":1,\"category\":0},{\"name\":\"IDBCursor\",\"value\":1,\"category\":4},{\"name\":\"IDBAny\",\"value\":1,\"category\":4},{\"name\":\"IDBKey\",\"value\":1,\"category\":4},{\"name\":\"IDBRequest\",\"value\":1,\"category\":4},{\"name\":\"IDBCursorWithValue\",\"value\":1,\"category\":4},{\"name\":\"IDBDatabase\",\"value\":1,\"category\":4},{\"name\":\"DOMStringList\",\"value\":1,\"category\":4},{\"name\":\"IDBObjectStore\",\"value\":1,\"category\":4},{\"name\":\"IDBTransaction\",\"value\":1,\"category\":4},{\"name\":\"IDBFactory\",\"value\":1,\"category\":4},{\"name\":\"IDBVersionChangeRequest\",\"value\":1,\"category\":4},{\"name\":\"IDBOpenDBRequest\",\"value\":1,\"category\":4},{\"name\":\"IDBIndex\",\"value\":1,\"category\":4},{\"name\":\"IDBKeyRange\",\"value\":1,\"category\":4},{\"name\":\"DOMError\",\"value\":1,\"category\":4},{\"name\":\"Int16Array\",\"value\":1,\"category\":4},{\"name\":\"Int32Array\",\"value\":1,\"category\":4},{\"name\":\"Int8Array\",\"value\":1,\"category\":4},{\"name\":\"JavaScriptCallFrame\",\"value\":1,\"category\":4},{\"name\":\"LocalMediaStream\",\"value\":1,\"category\":4},{\"name\":\"MediaStream\",\"value\":1,\"category\":4},{\"name\":\"Location\",\"value\":1,\"category\":4},{\"name\":\"MediaQueryList\",\"value\":1,\"category\":4},{\"name\":\"MediaQueryListListener\",\"value\":1,\"category\":4},{\"name\":\"MediaSource\",\"value\":1,\"category\":4},{\"name\":\"SourceBufferList\",\"value\":1,\"category\":4},{\"name\":\"SourceBuffer\",\"value\":1,\"category\":4},{\"name\":\"MediaStreamTrackList\",\"value\":1,\"category\":4},{\"name\":\"MediaStreamList\",\"value\":1,\"category\":4},{\"name\":\"MediaStreamTrack\",\"value\":1,\"category\":4},{\"name\":\"MessageChannel\",\"value\":1,\"category\":4},{\"name\":\"MessagePort\",\"value\":1,\"category\":4},{\"name\":\"MutationObserver\",\"value\":1,\"category\":4},{\"name\":\"MutationRecord\",\"value\":1,\"category\":4},{\"name\":\"Navigator\",\"value\":1,\"category\":4},{\"name\":\"BatteryManager\",\"value\":1,\"category\":4},{\"name\":\"NavigatorUserMediaErrorCallback\",\"value\":1,\"category\":4},{\"name\":\"NavigatorUserMediaError\",\"value\":1,\"category\":4},{\"name\":\"NavigatorUserMediaSuccessCallback\",\"value\":1,\"category\":4},{\"name\":\"NodeFilter\",\"value\":1,\"category\":4},{\"name\":\"NodeIterator\",\"value\":1,\"category\":4},{\"name\":\"Notation\",\"value\":1,\"category\":4},{\"name\":\"Notification\",\"value\":1,\"category\":4},{\"name\":\"NotificationPermissionCallback\",\"value\":1,\"category\":4},{\"name\":\"NotificationCenter\",\"value\":1,\"category\":4},{\"name\":\"OESVertexArrayObject\",\"value\":1,\"category\":4},{\"name\":\"WebGLVertexArrayObjectOES\",\"value\":1,\"category\":1},{\"name\":\"Performance\",\"value\":1,\"category\":4},{\"name\":\"PerformanceNavigation\",\"value\":1,\"category\":4},{\"name\":\"PerformanceTiming\",\"value\":1,\"category\":4},{\"name\":\"PositionErrorCallback\",\"value\":1,\"category\":4},{\"name\":\"PositionError\",\"value\":1,\"category\":4},{\"name\":\"ProcessingInstruction\",\"value\":1,\"category\":4},{\"name\":\"RadioNodeList\",\"value\":1,\"category\":4},{\"name\":\"RTCDataChannel\",\"value\":1,\"category\":4},{\"name\":\"RTCPeerConnection\",\"value\":1,\"category\":4},{\"name\":\"RTCSessionDescription\",\"value\":1,\"category\":4},{\"name\":\"RTCIceCandidate\",\"value\":1,\"category\":4},{\"name\":\"RTCSessionDescriptionCallback\",\"value\":1,\"category\":4},{\"name\":\"RTCStatsCallback\",\"value\":1,\"category\":4},{\"name\":\"RTCStatsResponse\",\"value\":1,\"category\":4},{\"name\":\"RTCStatsReport\",\"value\":1,\"category\":4},{\"name\":\"RTCStatsElement\",\"value\":1,\"category\":4},{\"name\":\"ScriptProfile\",\"value\":1,\"category\":4},{\"name\":\"ScriptProfileNode\",\"value\":1,\"category\":4},{\"name\":\"SharedWorker\",\"value\":1,\"category\":4},{\"name\":\"AbstractWorker\",\"value\":1,\"category\":4},{\"name\":\"SharedWorkerContext\",\"value\":1,\"category\":4},{\"name\":\"SpeechGrammarList\",\"value\":1,\"category\":4},{\"name\":\"SpeechGrammar\",\"value\":1,\"category\":4},{\"name\":\"SpeechInputResultList\",\"value\":1,\"category\":4},{\"name\":\"SpeechInputResult\",\"value\":1,\"category\":4},{\"name\":\"SpeechRecognition\",\"value\":1,\"category\":4},{\"name\":\"SpeechRecognitionResult\",\"value\":1,\"category\":4},{\"name\":\"SpeechRecognitionAlternative\",\"value\":1,\"category\":4},{\"name\":\"SpeechRecognitionResultList\",\"value\":1,\"category\":4},{\"name\":\"SQLResultSet\",\"value\":1,\"category\":4},{\"name\":\"SQLResultSetRowList\",\"value\":1,\"category\":4},{\"name\":\"SQLStatementCallback\",\"value\":1,\"category\":4},{\"name\":\"SQLTransaction\",\"value\":1,\"category\":4},{\"name\":\"SQLStatementErrorCallback\",\"value\":1,\"category\":4},{\"name\":\"SQLTransactionErrorCallback\",\"value\":1,\"category\":4},{\"name\":\"SQLError\",\"value\":1,\"category\":4},{\"name\":\"SQLTransactionSync\",\"value\":1,\"category\":4},{\"name\":\"StorageInfo\",\"value\":1,\"category\":4},{\"name\":\"StorageInfoUsageCallback\",\"value\":1,\"category\":4},{\"name\":\"StorageInfoQuotaCallback\",\"value\":1,\"category\":4},{\"name\":\"StorageInfoErrorCallback\",\"value\":1,\"category\":4},{\"name\":\"DOMCoreException\",\"value\":1,\"category\":4},{\"name\":\"StyleSheetList\",\"value\":1,\"category\":4},{\"name\":\"SVGAElement\",\"value\":1,\"category\":2},{\"name\":\"SVGTransformable\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimatedString\",\"value\":1,\"category\":2},{\"name\":\"SVGAltGlyphDefElement\",\"value\":1,\"category\":2},{\"name\":\"SVGElement\",\"value\":3,\"category\":2},{\"name\":\"SVGAltGlyphElement\",\"value\":1,\"category\":2},{\"name\":\"SVGURIReference\",\"value\":1,\"category\":2},{\"name\":\"SVGAltGlyphItemElement\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimateColorElement\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimationElement\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimatedAngle\",\"value\":1,\"category\":2},{\"name\":\"SVGAngle\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimatedLength\",\"value\":1,\"category\":2},{\"name\":\"SVGLength\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimatedLengthList\",\"value\":1,\"category\":2},{\"name\":\"SVGLengthList\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimatedNumberList\",\"value\":1,\"category\":2},{\"name\":\"SVGNumberList\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimatedPreserveAspectRatio\",\"value\":1,\"category\":2},{\"name\":\"SVGPreserveAspectRatio\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimatedRect\",\"value\":1,\"category\":2},{\"name\":\"SVGRect\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimatedTransformList\",\"value\":1,\"category\":2},{\"name\":\"SVGTransformList\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimateElement\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimateMotionElement\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimateTransformElement\",\"value\":1,\"category\":2},{\"name\":\"ElementTimeControl\",\"value\":1,\"category\":4},{\"name\":\"SVGCircleElement\",\"value\":1,\"category\":2},{\"name\":\"SVGClipPathElement\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimatedEnumeration\",\"value\":1,\"category\":2},{\"name\":\"SVGColor\",\"value\":1,\"category\":2},{\"name\":\"SVGComponentTransferFunctionElement\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimatedNumber\",\"value\":1,\"category\":2},{\"name\":\"SVGCursorElement\",\"value\":1,\"category\":2},{\"name\":\"SVGExternalResourcesRequired\",\"value\":1,\"category\":2},{\"name\":\"SVGDefsElement\",\"value\":1,\"category\":2},{\"name\":\"SVGDescElement\",\"value\":1,\"category\":2},{\"name\":\"SVGStylable\",\"value\":1,\"category\":2},{\"name\":\"SVGSVGElement\",\"value\":1,\"category\":2},{\"name\":\"SVGElementInstance\",\"value\":1,\"category\":2},{\"name\":\"EventTarget\",\"value\":1,\"category\":4},{\"name\":\"SVGElementInstanceList\",\"value\":1,\"category\":2},{\"name\":\"SVGUseElement\",\"value\":1,\"category\":2},{\"name\":\"SVGEllipseElement\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimatedBoolean\",\"value\":1,\"category\":2},{\"name\":\"SVGFEBlendElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFilterPrimitiveStandardAttributes\",\"value\":1,\"category\":2},{\"name\":\"SVGFEColorMatrixElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEComponentTransferElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFECompositeElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEConvolveMatrixElement\",\"value\":1,\"category\":2},{\"name\":\"SVGAnimatedInteger\",\"value\":1,\"category\":2},{\"name\":\"SVGFEDiffuseLightingElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEDisplacementMapElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEDistantLightElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEDropShadowElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEFloodElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEFuncAElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEFuncBElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEFuncGElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEFuncRElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEGaussianBlurElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEImageElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEMergeElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEMergeNodeElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEMorphologyElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEOffsetElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFEPointLightElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFESpecularLightingElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFESpotLightElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFETileElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFETurbulenceElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFilterElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFitToViewBox\",\"value\":1,\"category\":2},{\"name\":\"SVGFontElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFontFaceElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFontFaceFormatElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFontFaceNameElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFontFaceSrcElement\",\"value\":1,\"category\":2},{\"name\":\"SVGFontFaceUriElement\",\"value\":1,\"category\":2},{\"name\":\"SVGForeignObjectElement\",\"value\":1,\"category\":2},{\"name\":\"SVGGElement\",\"value\":1,\"category\":2},{\"name\":\"SVGGlyphElement\",\"value\":1,\"category\":2},{\"name\":\"SVGGlyphRefElement\",\"value\":1,\"category\":2},{\"name\":\"SVGGradientElement\",\"value\":1,\"category\":2},{\"name\":\"SVGHKernElement\",\"value\":1,\"category\":2},{\"name\":\"SVGImageElement\",\"value\":1,\"category\":2},{\"name\":\"SVGLinearGradientElement\",\"value\":1,\"category\":2},{\"name\":\"SVGLineElement\",\"value\":1,\"category\":2},{\"name\":\"SVGLocatable\",\"value\":1,\"category\":2},{\"name\":\"SVGMatrix\",\"value\":1,\"category\":2},{\"name\":\"SVGMarkerElement\",\"value\":1,\"category\":2},{\"name\":\"SVGMaskElement\",\"value\":1,\"category\":2},{\"name\":\"SVGMetadataElement\",\"value\":1,\"category\":2},{\"name\":\"SVGMissingGlyphElement\",\"value\":1,\"category\":2},{\"name\":\"SVGMPathElement\",\"value\":1,\"category\":2},{\"name\":\"SVGNumber\",\"value\":1,\"category\":2},{\"name\":\"SVGPaint\",\"value\":1,\"category\":2},{\"name\":\"SVGPathElement\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegList\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegArcAbs\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegArcRel\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegClosePath\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegCurvetoCubicAbs\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegCurvetoCubicRel\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegCurvetoCubicSmoothAbs\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegCurvetoCubicSmoothRel\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegCurvetoQuadraticAbs\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegCurvetoQuadraticRel\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegCurvetoQuadraticSmoothAbs\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegCurvetoQuadraticSmoothRel\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegLinetoAbs\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegLinetoHorizontalAbs\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegLinetoHorizontalRel\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegLinetoRel\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegLinetoVerticalAbs\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegLinetoVerticalRel\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegMovetoAbs\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSegMovetoRel\",\"value\":1,\"category\":2},{\"name\":\"SVGPoint\",\"value\":1,\"category\":2},{\"name\":\"SVGPathSeg\",\"value\":1,\"category\":2},{\"name\":\"SVGPatternElement\",\"value\":1,\"category\":2},{\"name\":\"SVGPointList\",\"value\":1,\"category\":2},{\"name\":\"SVGPolygonElement\",\"value\":1,\"category\":2},{\"name\":\"SVGPolylineElement\",\"value\":1,\"category\":2},{\"name\":\"SVGRadialGradientElement\",\"value\":1,\"category\":2},{\"name\":\"SVGRectElement\",\"value\":1,\"category\":2},{\"name\":\"SVGScriptElement\",\"value\":1,\"category\":2},{\"name\":\"SVGSetElement\",\"value\":1,\"category\":2},{\"name\":\"SVGStopElement\",\"value\":1,\"category\":2},{\"name\":\"SVGStyleElement\",\"value\":1,\"category\":2},{\"name\":\"SVGLangSpace\",\"value\":1,\"category\":2},{\"name\":\"SVGZoomAndPan\",\"value\":1,\"category\":2},{\"name\":\"SVGViewSpec\",\"value\":1,\"category\":2},{\"name\":\"SVGTransform\",\"value\":1,\"category\":2},{\"name\":\"SVGSwitchElement\",\"value\":1,\"category\":2},{\"name\":\"SVGSymbolElement\",\"value\":1,\"category\":2},{\"name\":\"SVGTests\",\"value\":1,\"category\":2},{\"name\":\"SVGStringList\",\"value\":1,\"category\":2},{\"name\":\"SVGTextContentElement\",\"value\":1,\"category\":2},{\"name\":\"SVGTextElement\",\"value\":1,\"category\":2},{\"name\":\"SVGTextPathElement\",\"value\":1,\"category\":2},{\"name\":\"SVGTextPositioningElement\",\"value\":1,\"category\":2},{\"name\":\"SVGTitleElement\",\"value\":1,\"category\":2},{\"name\":\"SVGTRefElement\",\"value\":1,\"category\":2},{\"name\":\"SVGTSpanElement\",\"value\":1,\"category\":2},{\"name\":\"SVGViewElement\",\"value\":1,\"category\":2},{\"name\":\"SVGVKernElement\",\"value\":1,\"category\":2},{\"name\":\"TextTrackCueList\",\"value\":1,\"category\":4},{\"name\":\"TextTrackCue\",\"value\":1,\"category\":4},{\"name\":\"Touch\",\"value\":1,\"category\":4},{\"name\":\"TouchList\",\"value\":1,\"category\":4},{\"name\":\"TreeWalker\",\"value\":1,\"category\":4},{\"name\":\"Uint16Array\",\"value\":1,\"category\":4},{\"name\":\"Uint32Array\",\"value\":1,\"category\":4},{\"name\":\"Uint8ClampedArray\",\"value\":1,\"category\":4},{\"name\":\"WebGLRenderingContext\",\"value\":3,\"category\":1},{\"name\":\"WebGLProgram\",\"value\":1,\"category\":1},{\"name\":\"WebGLBuffer\",\"value\":1,\"category\":1},{\"name\":\"WebGLFramebuffer\",\"value\":1,\"category\":1},{\"name\":\"WebGLRenderbuffer\",\"value\":1,\"category\":1},{\"name\":\"WebGLTexture\",\"value\":1,\"category\":1},{\"name\":\"WebGLShader\",\"value\":1,\"category\":1},{\"name\":\"WebGLActiveInfo\",\"value\":1,\"category\":1},{\"name\":\"WebGLContextAttributes\",\"value\":1,\"category\":1},{\"name\":\"WebGLShaderPrecisionFormat\",\"value\":1,\"category\":1},{\"name\":\"WebGLUniformLocation\",\"value\":1,\"category\":1},{\"name\":\"WebKitAnimationList\",\"value\":1,\"category\":4},{\"name\":\"WebKitAnimation\",\"value\":1,\"category\":4},{\"name\":\"WebKitCSSFilterValue\",\"value\":1,\"category\":4},{\"name\":\"WebKitCSSKeyframeRule\",\"value\":1,\"category\":4},{\"name\":\"WebKitCSSKeyframesRule\",\"value\":1,\"category\":4},{\"name\":\"WebKitCSSMatrix\",\"value\":1,\"category\":4},{\"name\":\"WebKitCSSMixFunctionValue\",\"value\":1,\"category\":4},{\"name\":\"WebKitCSSTransformValue\",\"value\":1,\"category\":4},{\"name\":\"WebKitNamedFlow\",\"value\":1,\"category\":4},{\"name\":\"WebSocket\",\"value\":1,\"category\":4},{\"name\":\"Worker\",\"value\":1,\"category\":4},{\"name\":\"WorkerLocation\",\"value\":1,\"category\":4},{\"name\":\"WorkerNavigator\",\"value\":1,\"category\":4},{\"name\":\"XMLHttpRequest\",\"value\":1,\"category\":4},{\"name\":\"XMLHttpRequestUpload\",\"value\":1,\"category\":4},{\"name\":\"DOMFormData\",\"value\":1,\"category\":4},{\"name\":\"XPathEvaluator\",\"value\":1,\"category\":4},{\"name\":\"XPathExpression\",\"value\":1,\"category\":4},{\"name\":\"XPathNSResolver\",\"value\":1,\"category\":4},{\"name\":\"XPathResult\",\"value\":1,\"category\":4},{\"name\":\"XSLTProcessor\",\"value\":1,\"category\":4}],\"links\":[{\"source\":0,\"target\":1},{\"source\":0,\"target\":2},{\"source\":0,\"target\":3},{\"source\":4,\"target\":4},{\"source\":5,\"target\":4},{\"source\":6,\"target\":7},{\"source\":6,\"target\":8},{\"source\":9,\"target\":3},{\"source\":10,\"target\":9},{\"source\":11,\"target\":12},{\"source\":11,\"target\":9},{\"source\":11,\"target\":13},{\"source\":11,\"target\":14},{\"source\":15,\"target\":16},{\"source\":15,\"target\":17},{\"source\":15,\"target\":0},{\"source\":15,\"target\":18},{\"source\":15,\"target\":9},{\"source\":15,\"target\":11},{\"source\":15,\"target\":19},{\"source\":15,\"target\":20},{\"source\":15,\"target\":21},{\"source\":15,\"target\":22},{\"source\":15,\"target\":23},{\"source\":15,\"target\":24},{\"source\":15,\"target\":25},{\"source\":15,\"target\":26},{\"source\":15,\"target\":27},{\"source\":15,\"target\":28},{\"source\":15,\"target\":29},{\"source\":15,\"target\":30},{\"source\":15,\"target\":31},{\"source\":15,\"target\":32},{\"source\":15,\"target\":4},{\"source\":16,\"target\":1},{\"source\":13,\"target\":14},{\"source\":1,\"target\":15},{\"source\":1,\"target\":1},{\"source\":1,\"target\":14},{\"source\":14,\"target\":3},{\"source\":12,\"target\":1},{\"source\":18,\"target\":1},{\"source\":18,\"target\":14},{\"source\":18,\"target\":3},{\"source\":33,\"target\":34},{\"source\":35,\"target\":33},{\"source\":35,\"target\":36},{\"source\":35,\"target\":37},{\"source\":35,\"target\":38},{\"source\":35,\"target\":39},{\"source\":35,\"target\":34},{\"source\":35,\"target\":40},{\"source\":35,\"target\":41},{\"source\":42,\"target\":43},{\"source\":19,\"target\":1},{\"source\":20,\"target\":1},{\"source\":44,\"target\":7},{\"source\":45,\"target\":46},{\"source\":47,\"target\":48},{\"source\":47,\"target\":49},{\"source\":47,\"target\":39},{\"source\":50,\"target\":44},{\"source\":51,\"target\":52},{\"source\":21,\"target\":1},{\"source\":21,\"target\":9},{\"source\":53,\"target\":5},{\"source\":54,\"target\":55},{\"source\":56,\"target\":55},{\"source\":56,\"target\":57},{\"source\":58,\"target\":55},{\"source\":58,\"target\":59},{\"source\":58,\"target\":60},{\"source\":61,\"target\":55},{\"source\":61,\"target\":62},{\"source\":61,\"target\":59},{\"source\":63,\"target\":55},{\"source\":63,\"target\":57},{\"source\":64,\"target\":65},{\"source\":64,\"target\":66},{\"source\":64,\"target\":67},{\"source\":64,\"target\":68},{\"source\":55,\"target\":55},{\"source\":55,\"target\":60},{\"source\":62,\"target\":55},{\"source\":57,\"target\":55},{\"source\":57,\"target\":65},{\"source\":69,\"target\":55},{\"source\":69,\"target\":57},{\"source\":60,\"target\":70},{\"source\":60,\"target\":62},{\"source\":60,\"target\":55},{\"source\":71,\"target\":55},{\"source\":72,\"target\":65},{\"source\":73,\"target\":74},{\"source\":75,\"target\":73},{\"source\":75,\"target\":76},{\"source\":76,\"target\":77},{\"source\":78,\"target\":79},{\"source\":78,\"target\":80},{\"source\":49,\"target\":81},{\"source\":49,\"target\":78},{\"source\":82,\"target\":5},{\"source\":83,\"target\":84},{\"source\":22,\"target\":1},{\"source\":22,\"target\":14},{\"source\":85,\"target\":80},{\"source\":85,\"target\":86},{\"source\":85,\"target\":87},{\"source\":88,\"target\":89},{\"source\":88,\"target\":90},{\"source\":88,\"target\":88},{\"source\":88,\"target\":91},{\"source\":86,\"target\":92},{\"source\":90,\"target\":93},{\"source\":94,\"target\":7},{\"source\":94,\"target\":8},{\"source\":94,\"target\":95},{\"source\":96,\"target\":7},{\"source\":96,\"target\":97},{\"source\":98,\"target\":85},{\"source\":99,\"target\":88},{\"source\":100,\"target\":60},{\"source\":100,\"target\":96},{\"source\":100,\"target\":101},{\"source\":102,\"target\":103},{\"source\":104,\"target\":102},{\"source\":103,\"target\":102},{\"source\":105,\"target\":103},{\"source\":106,\"target\":7},{\"source\":106,\"target\":107},{\"source\":108,\"target\":109},{\"source\":23,\"target\":1},{\"source\":23,\"target\":14},{\"source\":8,\"target\":7},{\"source\":8,\"target\":109},{\"source\":8,\"target\":110},{\"source\":8,\"target\":8},{\"source\":8,\"target\":57},{\"source\":8,\"target\":6},{\"source\":8,\"target\":46},{\"source\":8,\"target\":45},{\"source\":8,\"target\":95},{\"source\":8,\"target\":111},{\"source\":112,\"target\":7},{\"source\":113,\"target\":7},{\"source\":92,\"target\":114},{\"source\":80,\"target\":98},{\"source\":80,\"target\":85},{\"source\":80,\"target\":115},{\"source\":80,\"target\":116},{\"source\":80,\"target\":87},{\"source\":114,\"target\":80},{\"source\":93,\"target\":89},{\"source\":116,\"target\":80},{\"source\":89,\"target\":99},{\"source\":89,\"target\":89},{\"source\":89,\"target\":117},{\"source\":89,\"target\":88},{\"source\":118,\"target\":119},{\"source\":120,\"target\":81},{\"source\":121,\"target\":80},{\"source\":121,\"target\":122},{\"source\":121,\"target\":120},{\"source\":91,\"target\":89},{\"source\":91,\"target\":123},{\"source\":91,\"target\":81},{\"source\":48,\"target\":81},{\"source\":124,\"target\":119},{\"source\":125,\"target\":4},{\"source\":126,\"target\":98},{\"source\":127,\"target\":119},{\"source\":122,\"target\":127},{\"source\":3,\"target\":5},{\"source\":3,\"target\":3},{\"source\":128,\"target\":5},{\"source\":128,\"target\":128},{\"source\":24,\"target\":1},{\"source\":24,\"target\":13},{\"source\":129,\"target\":130},{\"source\":131,\"target\":132},{\"source\":133,\"target\":134},{\"source\":135,\"target\":7},{\"source\":135,\"target\":95},{\"source\":136,\"target\":137},{\"source\":138,\"target\":137},{\"source\":139,\"target\":137},{\"source\":140,\"target\":141},{\"source\":142,\"target\":137},{\"source\":143,\"target\":137},{\"source\":144,\"target\":137},{\"source\":145,\"target\":137},{\"source\":146,\"target\":137},{\"source\":146,\"target\":147},{\"source\":146,\"target\":95},{\"source\":146,\"target\":148},{\"source\":34,\"target\":137},{\"source\":149,\"target\":7},{\"source\":150,\"target\":137},{\"source\":150,\"target\":95},{\"source\":151,\"target\":137},{\"source\":151,\"target\":149},{\"source\":152,\"target\":137},{\"source\":153,\"target\":137},{\"source\":154,\"target\":137},{\"source\":155,\"target\":137},{\"source\":101,\"target\":8},{\"source\":101,\"target\":135},{\"source\":101,\"target\":149},{\"source\":137,\"target\":8},{\"source\":137,\"target\":149},{\"source\":156,\"target\":137},{\"source\":156,\"target\":157},{\"source\":158,\"target\":137},{\"source\":158,\"target\":149},{\"source\":158,\"target\":147},{\"source\":158,\"target\":148},{\"source\":159,\"target\":137},{\"source\":160,\"target\":149},{\"source\":160,\"target\":7},{\"source\":147,\"target\":137},{\"source\":147,\"target\":149},{\"source\":161,\"target\":137},{\"source\":161,\"target\":157},{\"source\":162,\"target\":137},{\"source\":163,\"target\":137},{\"source\":164,\"target\":137},{\"source\":165,\"target\":137},{\"source\":166,\"target\":137},{\"source\":167,\"target\":137},{\"source\":167,\"target\":157},{\"source\":39,\"target\":137},{\"source\":168,\"target\":137},{\"source\":168,\"target\":48},{\"source\":168,\"target\":147},{\"source\":168,\"target\":95},{\"source\":168,\"target\":148},{\"source\":168,\"target\":114},{\"source\":169,\"target\":137},{\"source\":169,\"target\":147},{\"source\":169,\"target\":95},{\"source\":169,\"target\":148},{\"source\":170,\"target\":137},{\"source\":170,\"target\":147},{\"source\":171,\"target\":137},{\"source\":171,\"target\":147},{\"source\":172,\"target\":137},{\"source\":173,\"target\":137},{\"source\":173,\"target\":70},{\"source\":173,\"target\":108},{\"source\":174,\"target\":137},{\"source\":174,\"target\":149},{\"source\":175,\"target\":137},{\"source\":141,\"target\":137},{\"source\":141,\"target\":176},{\"source\":141,\"target\":177},{\"source\":141,\"target\":178},{\"source\":141,\"target\":179},{\"source\":141,\"target\":180},{\"source\":181,\"target\":137},{\"source\":182,\"target\":137},{\"source\":183,\"target\":137},{\"source\":183,\"target\":95},{\"source\":184,\"target\":137},{\"source\":185,\"target\":137},{\"source\":185,\"target\":147},{\"source\":185,\"target\":148},{\"source\":185,\"target\":157},{\"source\":186,\"target\":137},{\"source\":187,\"target\":137},{\"source\":188,\"target\":137},{\"source\":188,\"target\":147},{\"source\":189,\"target\":149},{\"source\":189,\"target\":188},{\"source\":189,\"target\":7},{\"source\":190,\"target\":137},{\"source\":190,\"target\":147},{\"source\":190,\"target\":108},{\"source\":190,\"target\":95},{\"source\":190,\"target\":148},{\"source\":191,\"target\":137},{\"source\":192,\"target\":137},{\"source\":193,\"target\":137},{\"source\":194,\"target\":137},{\"source\":194,\"target\":95},{\"source\":195,\"target\":137},{\"source\":196,\"target\":137},{\"source\":197,\"target\":137},{\"source\":197,\"target\":147},{\"source\":197,\"target\":95},{\"source\":197,\"target\":189},{\"source\":197,\"target\":149},{\"source\":197,\"target\":148},{\"source\":197,\"target\":7},{\"source\":198,\"target\":137},{\"source\":199,\"target\":137},{\"source\":200,\"target\":137},{\"source\":201,\"target\":137},{\"source\":201,\"target\":70},{\"source\":202,\"target\":137},{\"source\":203,\"target\":137},{\"source\":204,\"target\":137},{\"source\":205,\"target\":137},{\"source\":205,\"target\":202},{\"source\":205,\"target\":149},{\"source\":205,\"target\":206},{\"source\":207,\"target\":137},{\"source\":207,\"target\":149},{\"source\":206,\"target\":137},{\"source\":206,\"target\":149},{\"source\":208,\"target\":137},{\"source\":208,\"target\":147},{\"source\":208,\"target\":95},{\"source\":208,\"target\":148},{\"source\":209,\"target\":137},{\"source\":210,\"target\":137},{\"source\":210,\"target\":180},{\"source\":211,\"target\":137},{\"source\":212,\"target\":137},{\"source\":40,\"target\":141},{\"source\":213,\"target\":214},{\"source\":213,\"target\":215},{\"source\":213,\"target\":216},{\"source\":217,\"target\":213},{\"source\":218,\"target\":219},{\"source\":218,\"target\":214},{\"source\":218,\"target\":220},{\"source\":218,\"target\":221},{\"source\":222,\"target\":215},{\"source\":222,\"target\":223},{\"source\":222,\"target\":224},{\"source\":222,\"target\":216},{\"source\":225,\"target\":214},{\"source\":225,\"target\":220},{\"source\":225,\"target\":216},{\"source\":226,\"target\":215},{\"source\":226,\"target\":226},{\"source\":220,\"target\":219},{\"source\":220,\"target\":214},{\"source\":220,\"target\":221},{\"source\":220,\"target\":216},{\"source\":220,\"target\":225},{\"source\":224,\"target\":216},{\"source\":216,\"target\":227},{\"source\":216,\"target\":214},{\"source\":216,\"target\":221},{\"source\":221,\"target\":218},{\"source\":221,\"target\":227},{\"source\":221,\"target\":220},{\"source\":223,\"target\":216},{\"source\":228,\"target\":5},{\"source\":228,\"target\":228},{\"source\":229,\"target\":5},{\"source\":229,\"target\":229},{\"source\":230,\"target\":5},{\"source\":230,\"target\":230},{\"source\":231,\"target\":231},{\"source\":232,\"target\":233},{\"source\":234,\"target\":219},{\"source\":177,\"target\":176},{\"source\":25,\"target\":12},{\"source\":25,\"target\":141},{\"source\":235,\"target\":236},{\"source\":236,\"target\":235},{\"source\":237,\"target\":238},{\"source\":237,\"target\":239},{\"source\":233,\"target\":240},{\"source\":26,\"target\":12},{\"source\":26,\"target\":233},{\"source\":27,\"target\":12},{\"source\":27,\"target\":233},{\"source\":241,\"target\":233},{\"source\":240,\"target\":242},{\"source\":243,\"target\":244},{\"source\":115,\"target\":117},{\"source\":245,\"target\":7},{\"source\":246,\"target\":95},{\"source\":246,\"target\":7},{\"source\":97,\"target\":7},{\"source\":247,\"target\":131},{\"source\":247,\"target\":104},{\"source\":247,\"target\":105},{\"source\":247,\"target\":248},{\"source\":247,\"target\":129},{\"source\":249,\"target\":250},{\"source\":251,\"target\":232},{\"source\":7,\"target\":97},{\"source\":7,\"target\":95},{\"source\":7,\"target\":7},{\"source\":7,\"target\":8},{\"source\":252,\"target\":7},{\"source\":253,\"target\":252},{\"source\":253,\"target\":7},{\"source\":95,\"target\":7},{\"source\":254,\"target\":7},{\"source\":255,\"target\":256},{\"source\":257,\"target\":255},{\"source\":257,\"target\":87},{\"source\":258,\"target\":259},{\"source\":28,\"target\":12},{\"source\":28,\"target\":14},{\"source\":28,\"target\":32},{\"source\":29,\"target\":1},{\"source\":260,\"target\":52},{\"source\":260,\"target\":261},{\"source\":260,\"target\":262},{\"source\":132,\"target\":133},{\"source\":263,\"target\":264},{\"source\":265,\"target\":7},{\"source\":265,\"target\":70},{\"source\":266,\"target\":95},{\"source\":107,\"target\":7},{\"source\":107,\"target\":94},{\"source\":107,\"target\":107},{\"source\":107,\"target\":46},{\"source\":107,\"target\":45},{\"source\":68,\"target\":64},{\"source\":67,\"target\":64},{\"source\":267,\"target\":4},{\"source\":267,\"target\":5},{\"source\":268,\"target\":269},{\"source\":268,\"target\":241},{\"source\":268,\"target\":270},{\"source\":268,\"target\":233},{\"source\":268,\"target\":271},{\"source\":268,\"target\":267},{\"source\":268,\"target\":272},{\"source\":271,\"target\":269},{\"source\":272,\"target\":273},{\"source\":274,\"target\":275},{\"source\":30,\"target\":1},{\"source\":276,\"target\":277},{\"source\":111,\"target\":94},{\"source\":111,\"target\":8},{\"source\":111,\"target\":7},{\"source\":111,\"target\":95},{\"source\":111,\"target\":106},{\"source\":278,\"target\":279},{\"source\":278,\"target\":244},{\"source\":280,\"target\":84},{\"source\":239,\"target\":176},{\"source\":239,\"target\":2},{\"source\":238,\"target\":239},{\"source\":281,\"target\":282},{\"source\":283,\"target\":284},{\"source\":285,\"target\":281},{\"source\":286,\"target\":287},{\"source\":288,\"target\":286},{\"source\":289,\"target\":290},{\"source\":291,\"target\":292},{\"source\":293,\"target\":292},{\"source\":74,\"target\":292},{\"source\":294,\"target\":295},{\"source\":296,\"target\":289},{\"source\":77,\"target\":296},{\"source\":297,\"target\":298},{\"source\":297,\"target\":299},{\"source\":300,\"target\":301},{\"source\":70,\"target\":59},{\"source\":70,\"target\":7},{\"source\":70,\"target\":70},{\"source\":302,\"target\":70},{\"source\":303,\"target\":304},{\"source\":303,\"target\":305},{\"source\":306,\"target\":307},{\"source\":308,\"target\":309},{\"source\":310,\"target\":307},{\"source\":311,\"target\":312},{\"source\":313,\"target\":314},{\"source\":315,\"target\":316},{\"source\":317,\"target\":318},{\"source\":319,\"target\":320},{\"source\":321,\"target\":322},{\"source\":323,\"target\":324},{\"source\":325,\"target\":326},{\"source\":327,\"target\":312},{\"source\":328,\"target\":312},{\"source\":329,\"target\":312},{\"source\":312,\"target\":330},{\"source\":312,\"target\":307},{\"source\":331,\"target\":304},{\"source\":331,\"target\":315},{\"source\":332,\"target\":304},{\"source\":332,\"target\":333},{\"source\":334,\"target\":65},{\"source\":334,\"target\":67},{\"source\":335,\"target\":307},{\"source\":335,\"target\":336},{\"source\":335,\"target\":319},{\"source\":335,\"target\":333},{\"source\":337,\"target\":338},{\"source\":337,\"target\":315},{\"source\":339,\"target\":304},{\"source\":340,\"target\":341},{\"source\":157,\"target\":342},{\"source\":307,\"target\":8},{\"source\":307,\"target\":342},{\"source\":307,\"target\":307},{\"source\":343,\"target\":344},{\"source\":343,\"target\":345},{\"source\":343,\"target\":307},{\"source\":343,\"target\":346},{\"source\":343,\"target\":343},{\"source\":345,\"target\":343},{\"source\":347,\"target\":304},{\"source\":347,\"target\":315},{\"source\":338,\"target\":348},{\"source\":349,\"target\":350},{\"source\":349,\"target\":305},{\"source\":349,\"target\":333},{\"source\":351,\"target\":350},{\"source\":351,\"target\":305},{\"source\":351,\"target\":333},{\"source\":351,\"target\":319},{\"source\":352,\"target\":350},{\"source\":352,\"target\":305},{\"source\":353,\"target\":350},{\"source\":353,\"target\":305},{\"source\":353,\"target\":336},{\"source\":353,\"target\":333},{\"source\":354,\"target\":350},{\"source\":354,\"target\":336},{\"source\":354,\"target\":333},{\"source\":354,\"target\":305},{\"source\":354,\"target\":319},{\"source\":354,\"target\":355},{\"source\":354,\"target\":348},{\"source\":356,\"target\":350},{\"source\":356,\"target\":336},{\"source\":356,\"target\":305},{\"source\":357,\"target\":350},{\"source\":357,\"target\":305},{\"source\":357,\"target\":336},{\"source\":357,\"target\":333},{\"source\":358,\"target\":307},{\"source\":358,\"target\":336},{\"source\":359,\"target\":350},{\"source\":359,\"target\":336},{\"source\":359,\"target\":305},{\"source\":360,\"target\":350},{\"source\":361,\"target\":335},{\"source\":362,\"target\":335},{\"source\":363,\"target\":335},{\"source\":364,\"target\":335},{\"source\":365,\"target\":350},{\"source\":365,\"target\":305},{\"source\":365,\"target\":336},{\"source\":366,\"target\":350},{\"source\":366,\"target\":321},{\"source\":367,\"target\":350},{\"source\":368,\"target\":307},{\"source\":368,\"target\":305},{\"source\":369,\"target\":350},{\"source\":369,\"target\":305},{\"source\":369,\"target\":333},{\"source\":369,\"target\":336},{\"source\":370,\"target\":350},{\"source\":370,\"target\":336},{\"source\":370,\"target\":305},{\"source\":371,\"target\":307},{\"source\":371,\"target\":336},{\"source\":372,\"target\":350},{\"source\":372,\"target\":305},{\"source\":372,\"target\":336},{\"source\":373,\"target\":307},{\"source\":373,\"target\":336},{\"source\":374,\"target\":350},{\"source\":374,\"target\":305},{\"source\":375,\"target\":350},{\"source\":375,\"target\":336},{\"source\":375,\"target\":355},{\"source\":375,\"target\":333},{\"source\":376,\"target\":341},{\"source\":376,\"target\":355},{\"source\":376,\"target\":333},{\"source\":376,\"target\":315},{\"source\":350,\"target\":341},{\"source\":350,\"target\":315},{\"source\":350,\"target\":305},{\"source\":377,\"target\":321},{\"source\":377,\"target\":323},{\"source\":378,\"target\":307},{\"source\":379,\"target\":307},{\"source\":380,\"target\":307},{\"source\":381,\"target\":307},{\"source\":382,\"target\":307},{\"source\":383,\"target\":307},{\"source\":384,\"target\":304},{\"source\":384,\"target\":315},{\"source\":385,\"target\":304},{\"source\":386,\"target\":307},{\"source\":387,\"target\":341},{\"source\":388,\"target\":341},{\"source\":388,\"target\":325},{\"source\":388,\"target\":333},{\"source\":389,\"target\":307},{\"source\":390,\"target\":304},{\"source\":390,\"target\":315},{\"source\":390,\"target\":321},{\"source\":318,\"target\":316},{\"source\":391,\"target\":388},{\"source\":391,\"target\":315},{\"source\":392,\"target\":304},{\"source\":392,\"target\":315},{\"source\":393,\"target\":307},{\"source\":393,\"target\":324},{\"source\":393,\"target\":394},{\"source\":395,\"target\":377},{\"source\":395,\"target\":315},{\"source\":395,\"target\":333},{\"source\":395,\"target\":313},{\"source\":395,\"target\":314},{\"source\":396,\"target\":341},{\"source\":396,\"target\":315},{\"source\":396,\"target\":333},{\"source\":394,\"target\":394},{\"source\":397,\"target\":307},{\"source\":398,\"target\":307},{\"source\":399,\"target\":338},{\"source\":320,\"target\":400},{\"source\":401,\"target\":334},{\"source\":402,\"target\":304},{\"source\":402,\"target\":403},{\"source\":402,\"target\":336},{\"source\":402,\"target\":404},{\"source\":402,\"target\":405},{\"source\":402,\"target\":406},{\"source\":402,\"target\":407},{\"source\":402,\"target\":408},{\"source\":402,\"target\":409},{\"source\":402,\"target\":410},{\"source\":402,\"target\":411},{\"source\":402,\"target\":412},{\"source\":402,\"target\":413},{\"source\":402,\"target\":414},{\"source\":402,\"target\":415},{\"source\":402,\"target\":416},{\"source\":402,\"target\":417},{\"source\":402,\"target\":418},{\"source\":402,\"target\":419},{\"source\":402,\"target\":420},{\"source\":402,\"target\":421},{\"source\":402,\"target\":422},{\"source\":402,\"target\":423},{\"source\":404,\"target\":424},{\"source\":405,\"target\":424},{\"source\":406,\"target\":424},{\"source\":407,\"target\":424},{\"source\":408,\"target\":424},{\"source\":409,\"target\":424},{\"source\":410,\"target\":424},{\"source\":411,\"target\":424},{\"source\":412,\"target\":424},{\"source\":413,\"target\":424},{\"source\":414,\"target\":424},{\"source\":415,\"target\":424},{\"source\":416,\"target\":424},{\"source\":417,\"target\":424},{\"source\":418,\"target\":424},{\"source\":419,\"target\":424},{\"source\":420,\"target\":424},{\"source\":403,\"target\":424},{\"source\":421,\"target\":424},{\"source\":422,\"target\":424},{\"source\":425,\"target\":377},{\"source\":425,\"target\":315},{\"source\":425,\"target\":333},{\"source\":425,\"target\":325},{\"source\":423,\"target\":423},{\"source\":426,\"target\":423},{\"source\":427,\"target\":304},{\"source\":427,\"target\":426},{\"source\":428,\"target\":304},{\"source\":428,\"target\":426},{\"source\":429,\"target\":388},{\"source\":429,\"target\":315},{\"source\":430,\"target\":304},{\"source\":430,\"target\":315},{\"source\":431,\"target\":338},{\"source\":432,\"target\":312},{\"source\":433,\"target\":341},{\"source\":433,\"target\":336},{\"source\":341,\"target\":305},{\"source\":341,\"target\":57},{\"source\":341,\"target\":65},{\"source\":434,\"target\":435},{\"source\":342,\"target\":436},{\"source\":342,\"target\":423},{\"source\":342,\"target\":437},{\"source\":342,\"target\":315},{\"source\":342,\"target\":324},{\"source\":342,\"target\":307},{\"source\":342,\"target\":314},{\"source\":342,\"target\":316},{\"source\":342,\"target\":394},{\"source\":342,\"target\":400},{\"source\":342,\"target\":438},{\"source\":342,\"target\":8},{\"source\":342,\"target\":95},{\"source\":439,\"target\":304},{\"source\":440,\"target\":377},{\"source\":441,\"target\":442},{\"source\":443,\"target\":341},{\"source\":443,\"target\":333},{\"source\":443,\"target\":315},{\"source\":443,\"target\":423},{\"source\":443,\"target\":324},{\"source\":444,\"target\":304},{\"source\":445,\"target\":309},{\"source\":445,\"target\":333},{\"source\":445,\"target\":315},{\"source\":446,\"target\":443},{\"source\":446,\"target\":317},{\"source\":446,\"target\":319},{\"source\":447,\"target\":341},{\"source\":438,\"target\":394},{\"source\":304,\"target\":393},{\"source\":304,\"target\":325},{\"source\":326,\"target\":438},{\"source\":448,\"target\":309},{\"source\":449,\"target\":446},{\"source\":309,\"target\":305},{\"source\":346,\"target\":304},{\"source\":346,\"target\":343},{\"source\":346,\"target\":315},{\"source\":450,\"target\":436},{\"source\":450,\"target\":442},{\"source\":437,\"target\":321},{\"source\":437,\"target\":326},{\"source\":437,\"target\":323},{\"source\":437,\"target\":307},{\"source\":451,\"target\":307},{\"source\":43,\"target\":44},{\"source\":43,\"target\":43},{\"source\":180,\"target\":452},{\"source\":180,\"target\":453},{\"source\":453,\"target\":180},{\"source\":453,\"target\":94},{\"source\":452,\"target\":453},{\"source\":179,\"target\":180},{\"source\":454,\"target\":344},{\"source\":455,\"target\":454},{\"source\":456,\"target\":7},{\"source\":456,\"target\":252},{\"source\":457,\"target\":5},{\"source\":457,\"target\":457},{\"source\":458,\"target\":5},{\"source\":458,\"target\":458},{\"source\":2,\"target\":5},{\"source\":2,\"target\":2},{\"source\":459,\"target\":2},{\"source\":459,\"target\":459},{\"source\":31,\"target\":1},{\"source\":31,\"target\":3},{\"source\":460,\"target\":33},{\"source\":460,\"target\":461},{\"source\":460,\"target\":462},{\"source\":460,\"target\":463},{\"source\":460,\"target\":464},{\"source\":460,\"target\":465},{\"source\":460,\"target\":4},{\"source\":460,\"target\":5},{\"source\":460,\"target\":466},{\"source\":460,\"target\":467},{\"source\":460,\"target\":468},{\"source\":460,\"target\":469},{\"source\":460,\"target\":470},{\"source\":460,\"target\":36},{\"source\":460,\"target\":39},{\"source\":460,\"target\":34},{\"source\":460,\"target\":40},{\"source\":460,\"target\":3},{\"source\":471,\"target\":472},{\"source\":473,\"target\":72},{\"source\":474,\"target\":55},{\"source\":474,\"target\":57},{\"source\":475,\"target\":55},{\"source\":475,\"target\":62},{\"source\":475,\"target\":474},{\"source\":476,\"target\":476},{\"source\":477,\"target\":72},{\"source\":478,\"target\":72},{\"source\":479,\"target\":95},{\"source\":480,\"target\":4},{\"source\":480,\"target\":5},{\"source\":481,\"target\":279},{\"source\":84,\"target\":222},{\"source\":84,\"target\":482},{\"source\":84,\"target\":483},{\"source\":84,\"target\":84},{\"source\":84,\"target\":257},{\"source\":84,\"target\":73},{\"source\":84,\"target\":76},{\"source\":84,\"target\":126},{\"source\":84,\"target\":99},{\"source\":84,\"target\":89},{\"source\":484,\"target\":485},{\"source\":484,\"target\":4},{\"source\":484,\"target\":5},{\"source\":484,\"target\":486},{\"source\":487,\"target\":488},{\"source\":487,\"target\":489},{\"source\":487,\"target\":490},{\"source\":488,\"target\":490},{\"source\":490,\"target\":7},{\"source\":491,\"target\":7},{\"source\":491,\"target\":94}]};\n  const option = {\n    legend: {\n      data: ['HTMLElement', 'WebGL', 'SVG', 'CSS', 'Other']\n    },\n    series: [{\n      type: 'graph',\n      layout: 'force',\n      animation: false,\n      label: {\n        normal: {\n          position: 'right',\n          formatter: '{b}'\n        }\n      },\n      draggable: true,\n      data: webkitDep.nodes.map(function (node, idx) {\n        node.id = idx;\n        return node;\n      }),\n      categories: webkitDep.categories,\n      force: {\n        // initLayout: 'circular'\n        // repulsion: 20,\n        edgeLength: 5,\n        repulsion: 20,\n        gravity: 0.2\n      },\n      edges: webkitDep.links\n    }]\n  };\n\n  return <ReactECharts\n    option={option}\n    style={{ height: '700px', width: '100%' }}\n  />;\n};\n\nexport default Page;"}},"dependencies":{"react":{"version":"^15.0.0 || >=16.0.0"},"echarts-for-react":{"version":"3.0.6"},"echarts":{"version":"^3.0.0 || ^4.0.0 || ^5.0.0 || ^6.0.0"}},"identifier":"graph-demo"},
  },
  'html-props-demo': {
    component: function DumiDemo() {
  var _interopRequireDefault = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireDefault.js")["default"];

  var _react = _interopRequireDefault(require("react"));

  var _echartsForReact = _interopRequireDefault(require("echarts-for-react"));

  var Page = function Page() {
    var option = {
      title: {
        text: '堆叠区域图'
      },
      tooltip: {
        trigger: 'axis'
      },
      legend: {
        data: ['邮件营销', '联盟广告', '视频广告']
      },
      toolbox: {
        feature: {
          saveAsImage: {}
        }
      },
      grid: {
        left: '3%',
        right: '4%',
        bottom: '3%',
        containLabel: true
      },
      xAxis: [{
        type: 'category',
        boundaryGap: false,
        data: ['周一', '周二', '周三', '周四', '周五', '周六', '周日']
      }],
      yAxis: [{
        type: 'value'
      }],
      series: [{
        name: '邮件营销',
        type: 'line',
        stack: '总量',
        areaStyle: {
          normal: {}
        },
        data: [120, 132, 101, 134, 90, 230, 210]
      }, {
        name: '联盟广告',
        type: 'line',
        stack: '总量',
        areaStyle: {
          normal: {}
        },
        data: [220, 182, 191, 234, 290, 330, 310]
      }, {
        name: '视频广告',
        type: 'line',
        stack: '总量',
        areaStyle: {
          normal: {}
        },
        data: [150, 232, 201, 154, 190, 330, 410]
      }]
    };

    var handleDemoButton = function handleDemoButton() {
      console.log(document.querySelector(['[data-testid="html-props-demo"]']));
      window.alert('Open console, see the log detail.');
    };

    return /*#__PURE__*/_react["default"].createElement(_react["default"].Fragment, null, /*#__PURE__*/_react["default"].createElement(_echartsForReact["default"], {
      option: option,
      style: {
        height: 400
      },
      role: "figure",
      "data-testid": "html-props-demo"
    }), /*#__PURE__*/_react["default"].createElement("button", {
      type: "button",
      onClick: handleDemoButton
    }, "Demo"));
  };

  var _default;

  return _react["default"].createElement(Page);
},
    previewerProps: {"sources":{"_":{"tsx":"import React from 'react';\nimport ReactECharts from 'echarts-for-react';\n\nconst Page: React.FC = () => {\n  const option = {\n    title: {\n      text: '堆叠区域图'\n    },\n    tooltip : {\n      trigger: 'axis'\n    },\n    legend: {\n      data:['邮件营销','联盟广告','视频广告']\n    },\n    toolbox: {\n      feature: {\n        saveAsImage: {}\n      }\n    },\n    grid: {\n      left: '3%',\n      right: '4%',\n      bottom: '3%',\n      containLabel: true\n    },\n    xAxis : [\n      {\n        type : 'category',\n        boundaryGap : false,\n        data : ['周一','周二','周三','周四','周五','周六','周日']\n      }\n    ],\n    yAxis : [\n      {\n        type : 'value'\n      }\n    ],\n    series : [\n      {\n        name:'邮件营销',\n        type:'line',\n        stack: '总量',\n        areaStyle: {normal: {}},\n        data:[120, 132, 101, 134, 90, 230, 210]\n      },\n      {\n        name:'联盟广告',\n        type:'line',\n        stack: '总量',\n        areaStyle: {normal: {}},\n        data:[220, 182, 191, 234, 290, 330, 310]\n      },\n      {\n        name:'视频广告',\n        type:'line',\n        stack: '总量',\n        areaStyle: {normal: {}},\n        data:[150, 232, 201, 154, 190, 330, 410]\n      }\n    ]\n  };\n\n  const handleDemoButton = () => {\n    console.log(document.querySelector(['[data-testid=\"html-props-demo\"]']));\n    window.alert('Open console, see the log detail.')\n  };\n\n  return (\n    <>\n      <ReactECharts\n        option={option}\n        style={{ height: 400 }}\n        role=\"figure\"\n        data-testid=\"html-props-demo\"\n      />\n      <button type=\"button\" onClick={handleDemoButton}>Demo</button>\n    </>\n  );\n};\n\nexport default Page;"}},"dependencies":{"react":{"version":"^15.0.0 || >=16.0.0"},"echarts-for-react":{"version":"3.0.6"},"echarts":{"version":"^3.0.0 || ^4.0.0 || ^5.0.0 || ^6.0.0"}},"identifier":"html-props-demo"},
  },
  'loading-demo': {
    component: function DumiDemo() {
  var _interopRequireDefault = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireDefault.js")["default"];

  var _interopRequireWildcard = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireWildcard.js")["default"];

  var _react = _interopRequireWildcard(require("react"));

  var _echartsForReact = _interopRequireDefault(require("echarts-for-react"));

  var Page = function Page() {
    var option = {
      title: {
        text: '基础雷达图'
      },
      tooltip: {},
      legend: {
        data: ['预算分配（Allocated Budget）', '实际开销（Actual Spending）']
      },
      radar: {
        // shape: 'circle',
        indicator: [{
          name: '销售（sales）',
          max: 6500
        }, {
          name: '管理（Administration）',
          max: 16000
        }, {
          name: '信息技术（Information Techology）',
          max: 30000
        }, {
          name: '客服（Customer Support）',
          max: 38000
        }, {
          name: '研发（Development）',
          max: 52000
        }, {
          name: '市场（Marketing）',
          max: 25000
        }]
      },
      series: [{
        name: '预算 vs 开销（Budget vs spending）',
        type: 'radar',
        // areaStyle: {normal: {}},
        data: [{
          value: [4300, 10000, 28000, 35000, 50000, 19000],
          name: '预算分配（Allocated Budget）'
        }, {
          value: [5000, 14000, 28000, 31000, 42000, 21000],
          name: '实际开销（Actual Spending）'
        }]
      }]
    };
    var timer;
    (0, _react.useEffect)(function () {
      return function () {
        return clearTimeout(timer);
      };
    });
    var loadingOption = {
      text: '加载中...',
      color: '#4413c2',
      textColor: '#270240',
      maskColor: 'rgba(194, 88, 86, 0.3)',
      zlevel: 0
    };

    function onChartReady(echarts) {
      timer = setTimeout(function () {
        echarts.hideLoading();
      }, 3000);
    }

    return /*#__PURE__*/_react["default"].createElement(_echartsForReact["default"], {
      option: option,
      style: {
        height: 400
      },
      onChartReady: onChartReady,
      loadingOption: loadingOption,
      showLoading: true
    });
  };

  var _default;

  return _react["default"].createElement(Page);
},
    previewerProps: {"sources":{"_":{"tsx":"import React, { useState, useEffect } from 'react';\nimport ReactECharts from 'echarts-for-react';\n\nconst Page: React.FC = () => {\n  const option = {\n    title: {\n      text: '基础雷达图'\n    },\n    tooltip: {},\n    legend: {\n      data: ['预算分配（Allocated Budget）', '实际开销（Actual Spending）']\n    },\n    radar: {\n      // shape: 'circle',\n      indicator: [\n          { name: '销售（sales）', max: 6500},\n          { name: '管理（Administration）', max: 16000},\n          { name: '信息技术（Information Techology）', max: 30000},\n          { name: '客服（Customer Support）', max: 38000},\n          { name: '研发（Development）', max: 52000},\n          { name: '市场（Marketing）', max: 25000}\n      ]\n    },\n    series: [{\n      name: '预算 vs 开销（Budget vs spending）',\n      type: 'radar',\n      // areaStyle: {normal: {}},\n      data : [\n        {\n          value : [4300, 10000, 28000, 35000, 50000, 19000],\n          name : '预算分配（Allocated Budget）'\n        },\n          {\n          value : [5000, 14000, 28000, 31000, 42000, 21000],\n          name : '实际开销（Actual Spending）'\n        }\n      ]\n    }]\n  };\n\n  let timer;\n\n  useEffect(() => {\n    return () => clearTimeout(timer);\n  });\n\n  const loadingOption = {\n    text: '加载中...',\n    color: '#4413c2',\n    textColor: '#270240',\n    maskColor: 'rgba(194, 88, 86, 0.3)',\n    zlevel: 0\n  };\n\n  function onChartReady(echarts) {\n    timer = setTimeout(function() {\n      echarts.hideLoading();\n    }, 3000);\n  }\n\n  return <ReactECharts\n    option={option}\n    style={{ height: 400 }}\n    onChartReady={onChartReady}\n    loadingOption={loadingOption}\n    showLoading={true}\n  />;\n};\n\nexport default Page;"}},"dependencies":{"react":{"version":"^15.0.0 || >=16.0.0"},"echarts-for-react":{"version":"3.0.6"},"echarts":{"version":"^3.0.0 || ^4.0.0 || ^5.0.0 || ^6.0.0"}},"identifier":"loading-demo"},
  },
  'locale-demo': {
    component: function DumiDemo() {
  var _interopRequireDefault = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireDefault.js")["default"];

  var _react = _interopRequireDefault(require("react"));

  var _echartsForReact = _interopRequireDefault(require("echarts-for-react"));

  require("echarts/i18n/langFR");

  var Page = function Page() {
    var option = {
      title: {
        text: 'ECharts 入门示例'
      },
      toolbox: {
        feature: {
          saveAsImage: {},
          dataZoom: {},
          restore: {}
        }
      },
      tooltip: {},
      legend: {
        data: ['销量']
      },
      xAxis: {
        data: ['衬衫', '羊毛衫', '雪纺衫', '裤子', '高跟鞋', '袜子']
      },
      yAxis: {},
      series: [{
        name: '销量',
        type: 'line',
        data: [5, 20, 36, 10, 10, 20]
      }]
    };
    return /*#__PURE__*/_react["default"].createElement(_echartsForReact["default"], {
      option: option,
      style: {
        height: 400
      },
      opts: {
        locale: 'FR'
      }
    });
  };

  var _default;

  return _react["default"].createElement(Page);
},
    previewerProps: {"sources":{"_":{"tsx":"import React from 'react';\nimport ReactECharts from 'echarts-for-react';\n\nimport \"echarts/i18n/langFR\";\n\nconst Page: React.FC = () => {\n  const option = {\n    title: {\n      text: 'ECharts 入门示例'\n    },\n    toolbox: {\n        feature: {\n            saveAsImage: {},\n            dataZoom: {},\n            restore: {}\n        }\n    },\n    tooltip: {},\n    legend: {\n      data:['销量']\n    },\n    xAxis: {\n      data: ['衬衫', '羊毛衫', '雪纺衫', '裤子', '高跟鞋', '袜子']\n    },\n    yAxis: {},\n    series: [{\n      name: '销量',\n      type: 'line',\n      data: [5, 20, 36, 10, 10, 20]\n    }]\n  };\n\n  return <ReactECharts\n    option={option}\n    style={{ height: 400 }}\n    opts={{ locale: 'FR' }}\n  />;\n};\n\nexport default Page;"}},"dependencies":{"react":{"version":"^15.0.0 || >=16.0.0"},"echarts-for-react":{"version":"3.0.6"},"echarts":{"version":"^3.0.0 || ^4.0.0 || ^5.0.0 || ^6.0.0"}},"identifier":"locale-demo"},
  },
  'simple-demo': {
    component: function DumiDemo() {
  var _interopRequireDefault = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireDefault.js")["default"];

  var _react = _interopRequireDefault(require("react"));

  var _echartsForReact = _interopRequireDefault(require("echarts-for-react"));

  var Page = function Page() {
    var option = {
      title: {
        text: '堆叠区域图'
      },
      tooltip: {
        trigger: 'axis'
      },
      legend: {
        data: ['邮件营销', '联盟广告', '视频广告']
      },
      toolbox: {
        feature: {
          saveAsImage: {}
        }
      },
      grid: {
        left: '3%',
        right: '4%',
        bottom: '15%',
        containLabel: true
      },
      xAxis: [{
        type: 'category',
        boundaryGap: false,
        data: ['周一', '周二', '周三', '周四', '周五', '周六', '周日']
      }],
      yAxis: [{
        type: 'value'
      }],
      series: [{
        name: '邮件营销',
        type: 'line',
        stack: '总量',
        areaStyle: {
          normal: {}
        },
        data: [120, 132, 101, 134, 90, 230, 210]
      }, {
        name: '联盟广告',
        type: 'line',
        stack: '总量',
        areaStyle: {
          normal: {}
        },
        data: [220, 182, 191, 234, 290, 330, 310]
      }, {
        name: '视频广告',
        type: 'line',
        stack: '总量',
        areaStyle: {
          normal: {}
        },
        data: [150, 232, 201, 154, 190, 330, 410]
      }]
    };
    return /*#__PURE__*/_react["default"].createElement(_echartsForReact["default"], {
      option: option,
      style: {
        height: 400
      }
    });
  };

  var _default;

  return _react["default"].createElement(Page);
},
    previewerProps: {"sources":{"_":{"tsx":"import React from 'react';\nimport ReactECharts from 'echarts-for-react';\n\nconst Page: React.FC = () => {\n  const option = {\n    title: {\n      text: '堆叠区域图',\n    },\n    tooltip: {\n      trigger: 'axis',\n    },\n    legend: {\n      data: ['邮件营销', '联盟广告', '视频广告'],\n    },\n    toolbox: {\n      feature: {\n        saveAsImage: {},\n      },\n    },\n    grid: {\n      left: '3%',\n      right: '4%',\n      bottom: '15%',\n      containLabel: true,\n    },\n    xAxis: [\n      {\n        type: 'category',\n        boundaryGap: false,\n        data: ['周一', '周二', '周三', '周四', '周五', '周六', '周日'],\n      },\n    ],\n    yAxis: [\n      {\n        type: 'value',\n      },\n    ],\n    series: [\n      {\n        name: '邮件营销',\n        type: 'line',\n        stack: '总量',\n        areaStyle: { normal: {} },\n        data: [120, 132, 101, 134, 90, 230, 210],\n      },\n      {\n        name: '联盟广告',\n        type: 'line',\n        stack: '总量',\n        areaStyle: { normal: {} },\n        data: [220, 182, 191, 234, 290, 330, 310],\n      },\n      {\n        name: '视频广告',\n        type: 'line',\n        stack: '总量',\n        areaStyle: { normal: {} },\n        data: [150, 232, 201, 154, 190, 330, 410],\n      },\n    ],\n  };\n\n  return <ReactECharts option={option} style={{ height: 400 }} />;\n};\n\nexport default Page;"}},"dependencies":{"react":{"version":"^15.0.0 || >=16.0.0"},"echarts-for-react":{"version":"3.0.6"},"echarts":{"version":"^3.0.0 || ^4.0.0 || ^5.0.0 || ^6.0.0"}},"identifier":"simple-demo"},
  },
  'svg-demo': {
    component: function DumiDemo() {
  var _interopRequireDefault = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireDefault.js")["default"];

  var _react = _interopRequireDefault(require("react"));

  var _echartsForReact = _interopRequireDefault(require("echarts-for-react"));

  var Page = function Page() {
    var option = {
      title: {
        text: 'ECharts 入门示例'
      },
      tooltip: {},
      legend: {
        data: ['销量']
      },
      xAxis: {
        data: ['衬衫', '羊毛衫', '雪纺衫', '裤子', '高跟鞋', '袜子']
      },
      yAxis: {},
      series: [{
        name: '销量',
        type: 'bar',
        data: [5, 20, 36, 10, 10, 20]
      }]
    };
    return /*#__PURE__*/_react["default"].createElement(_echartsForReact["default"], {
      option: option,
      style: {
        height: 400
      },
      opts: {
        renderer: 'svg'
      }
    });
  };

  var _default;

  return _react["default"].createElement(Page);
},
    previewerProps: {"sources":{"_":{"tsx":"import React from 'react';\nimport ReactECharts from 'echarts-for-react';\n\nconst Page: React.FC = () => {\n  const option = {\n    title: {\n      text: 'ECharts 入门示例'\n    },\n    tooltip: {},\n    legend: {\n      data:['销量']\n    },\n    xAxis: {\n      data: ['衬衫', '羊毛衫', '雪纺衫', '裤子', '高跟鞋', '袜子']\n    },\n    yAxis: {},\n    series: [{\n      name: '销量',\n      type: 'bar',\n      data: [5, 20, 36, 10, 10, 20]\n    }]\n  };\n\n  return <ReactECharts\n    option={option}\n    style={{ height: 400 }}\n    opts={{ renderer: 'svg' }}\n  />;\n};\n\nexport default Page;"}},"dependencies":{"react":{"version":"^15.0.0 || >=16.0.0"},"echarts-for-react":{"version":"3.0.6"},"echarts":{"version":"^3.0.0 || ^4.0.0 || ^5.0.0 || ^6.0.0"}},"identifier":"svg-demo"},
  },
  'theme-demo': {
    component: function DumiDemo() {
  var _interopRequireWildcard = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireWildcard.js")["default"];

  var _interopRequireDefault = require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/interopRequireDefault.js")["default"];

  var _slicedToArray2 = _interopRequireDefault(require("/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@babel_runtime@7.18.6@@babel/runtime/helpers/esm/slicedToArray.js"));

  var _react = _interopRequireWildcard(require("react"));

  var echarts = _interopRequireWildcard(require("echarts"));

  var _echartsForReact = _interopRequireDefault(require("echarts-for-react"));

  echarts.registerTheme('my_theme', {
    backgroundColor: '#f4cccc'
  });
  echarts.registerTheme('another_theme', {
    backgroundColor: '#eee'
  });

  var Page = function Page() {
    var option = {
      title: {
        text: '阶梯瀑布图',
        subtext: 'From ExcelHome',
        sublink: 'http://e.weibo.com/1341556070/Aj1J2x5a5'
      },
      tooltip: {
        trigger: 'axis',
        axisPointer: {
          // 坐标轴指示器，坐标轴触发有效
          type: 'shadow' // 默认为直线，可选为：'line' | 'shadow'

        }
      },
      legend: {
        data: ['支出', '收入']
      },
      grid: {
        left: '3%',
        right: '4%',
        bottom: '15%',
        containLabel: true
      },
      xAxis: {
        type: 'category',
        splitLine: {
          show: false
        },
        data: ['11月1日', '11月2日', '11月3日', '11月4日', '11月5日', '11月6日', '11月7日', '11月8日', '11月9日', '11月10日', '11月11日']
      },
      yAxis: {
        type: 'value'
      },
      series: [{
        name: '辅助',
        type: 'bar',
        stack: '总量',
        itemStyle: {
          normal: {
            barBorderColor: 'rgba(0,0,0,0)',
            color: 'rgba(0,0,0,0)'
          },
          emphasis: {
            barBorderColor: 'rgba(0,0,0,0)',
            color: 'rgba(0,0,0,0)'
          }
        },
        data: [0, 900, 1245, 1530, 1376, 1376, 1511, 1689, 1856, 1495, 1292]
      }, {
        name: '收入',
        type: 'bar',
        stack: '总量',
        label: {
          normal: {
            show: true,
            position: 'top'
          }
        },
        data: [900, 345, 393, '-', '-', 135, 178, 286, '-', '-', '-']
      }, {
        name: '支出',
        type: 'bar',
        stack: '总量',
        label: {
          normal: {
            show: true,
            position: 'bottom'
          }
        },
        data: ['-', '-', '-', 108, 154, '-', '-', '-', 119, 361, 203]
      }]
    };

    var _useState = (0, _react.useState)(),
        _useState2 = (0, _slicedToArray2["default"])(_useState, 2),
        theme = _useState2[0],
        setTheme = _useState2[1];

    var _useState3 = (0, _react.useState)('class_1'),
        _useState4 = (0, _slicedToArray2["default"])(_useState3, 2),
        className = _useState4[0],
        setClassName = _useState4[1];

    function toggleTheme() {
      setTheme(theme === 'my_theme' ? 'another_theme' : 'my_theme');
    }

    function toggleClassName() {
      setClassName(className === 'class_1' ? 'class_2' : 'class_1');
    }

    return /*#__PURE__*/_react["default"].createElement(_react["default"].Fragment, null, /*#__PURE__*/_react["default"].createElement(_echartsForReact["default"], {
      option: option,
      className: className,
      theme: theme,
      style: {
        height: 400
      }
    }), /*#__PURE__*/_react["default"].createElement("br", null), /*#__PURE__*/_react["default"].createElement("div", null, /*#__PURE__*/_react["default"].createElement("button", {
      onClick: toggleTheme
    }, "Click to Toggle theme."), /*#__PURE__*/_react["default"].createElement("button", {
      onClick: toggleClassName
    }, "Click to Toggle className.")));
  };

  var _default;

  return _react["default"].createElement(Page);
},
    previewerProps: {"sources":{"_":{"tsx":"import React, { useState } from 'react';\nimport * as echarts from 'echarts';\nimport ReactECharts from 'echarts-for-react';\n\necharts.registerTheme('my_theme', {\n  backgroundColor: '#f4cccc',\n});\necharts.registerTheme('another_theme', {\n  backgroundColor: '#eee',\n});\n\nconst Page: React.FC = () => {\n  const option = {\n    title: {\n      text: '阶梯瀑布图',\n      subtext: 'From ExcelHome',\n      sublink: 'http://e.weibo.com/1341556070/Aj1J2x5a5',\n    },\n    tooltip: {\n      trigger: 'axis',\n      axisPointer: {\n        // 坐标轴指示器，坐标轴触发有效\n        type: 'shadow', // 默认为直线，可选为：'line' | 'shadow'\n      },\n    },\n    legend: {\n      data: ['支出', '收入'],\n    },\n    grid: {\n      left: '3%',\n      right: '4%',\n      bottom: '15%',\n      containLabel: true,\n    },\n    xAxis: {\n      type: 'category',\n      splitLine: { show: false },\n      data: [\n        '11月1日',\n        '11月2日',\n        '11月3日',\n        '11月4日',\n        '11月5日',\n        '11月6日',\n        '11月7日',\n        '11月8日',\n        '11月9日',\n        '11月10日',\n        '11月11日',\n      ],\n    },\n    yAxis: {\n      type: 'value',\n    },\n    series: [\n      {\n        name: '辅助',\n        type: 'bar',\n        stack: '总量',\n        itemStyle: {\n          normal: {\n            barBorderColor: 'rgba(0,0,0,0)',\n            color: 'rgba(0,0,0,0)',\n          },\n          emphasis: {\n            barBorderColor: 'rgba(0,0,0,0)',\n            color: 'rgba(0,0,0,0)',\n          },\n        },\n        data: [0, 900, 1245, 1530, 1376, 1376, 1511, 1689, 1856, 1495, 1292],\n      },\n      {\n        name: '收入',\n        type: 'bar',\n        stack: '总量',\n        label: {\n          normal: {\n            show: true,\n            position: 'top',\n          },\n        },\n        data: [900, 345, 393, '-', '-', 135, 178, 286, '-', '-', '-'],\n      },\n      {\n        name: '支出',\n        type: 'bar',\n        stack: '总量',\n        label: {\n          normal: {\n            show: true,\n            position: 'bottom',\n          },\n        },\n        data: ['-', '-', '-', 108, 154, '-', '-', '-', 119, 361, 203],\n      },\n    ],\n  };\n\n  const [theme, setTheme] = useState();\n  const [className, setClassName] = useState('class_1');\n\n  function toggleTheme() {\n    setTheme(theme === 'my_theme' ? 'another_theme' : 'my_theme');\n  }\n\n  function toggleClassName() {\n    setClassName(className === 'class_1' ? 'class_2' : 'class_1');\n  }\n\n  return (\n    <>\n      <ReactECharts option={option} className={className} theme={theme} style={{ height: 400 }} />\n      <br />\n      <div>\n        <button onClick={toggleTheme}>Click to Toggle theme.</button>\n        <button onClick={toggleClassName}>Click to Toggle className.</button>\n      </div>\n    </>\n  );\n};\n\nexport default Page;"}},"dependencies":{"react":{"version":"^15.0.0 || >=16.0.0"},"echarts":{"version":"^3.0.0 || ^4.0.0 || ^5.0.0 || ^6.0.0"},"echarts-for-react":{"version":"3.0.6"}},"identifier":"theme-demo"},
  },
};
