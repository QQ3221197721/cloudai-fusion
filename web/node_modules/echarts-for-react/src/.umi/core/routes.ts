// @ts-nocheck
import React from 'react';
import { ApplyPluginsType } from '/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@umijs_runtime@3.5.43@@umijs/runtime';
import * as umiExports from './umiExports';
import { plugin } from './plugin';

export function getRoutes() {
  const routes = [
  {
    "path": "/~demos/:uuid",
    "layout": false,
    "wrappers": [require('../dumi/layout').default],
    "component": ((props) => {
        const React = require('react');
        const { default: getDemoRenderArgs } = require('/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_@umijs_preset-dumi@1.1.54@@umijs/preset-dumi/lib/plugins/features/demo/getDemoRenderArgs');
        const { default: Previewer } = require('dumi-theme-default/es/builtins/Previewer.js');
        const { usePrefersColor, context } = require('dumi/theme');

        
      const { demos } = React.useContext(context);
      const [renderArgs, setRenderArgs] = React.useState([]);

      // update render args when props changed
      React.useLayoutEffect(() => {
        setRenderArgs(getDemoRenderArgs(props, demos));
      }, [props.match.params.uuid, props.location.query.wrapper, props.location.query.capture]);

      // for listen prefers-color-schema media change in demo single route
      usePrefersColor();

      switch (renderArgs.length) {
        case 1:
          // render demo directly
          return renderArgs[0];

        case 2:
          // render demo with previewer
          return React.createElement(
            Previewer,
            renderArgs[0],
            renderArgs[1],
          );

        default:
          return `Demo ${props.match.params.uuid} not found :(`;
      }
    
        })
  },
  {
    "path": "/_demos/:uuid",
    "redirect": "/~demos/:uuid"
  },
  {
    "__dumiRoot": true,
    "layout": false,
    "path": "/",
    "wrappers": [require('../dumi/layout').default, require('/Users/xiaowei/Documents/code/antv/echarts-for-react/node_modules/_dumi-theme-default@1.1.24@dumi-theme-default/es/layout.js').default],
    "routes": [
      {
        "path": "/",
        "component": require('/Users/xiaowei/Documents/code/antv/echarts-for-react/docs/index.md').default,
        "exact": true,
        "meta": {
          "filePath": "docs/index.md",
          "updatedTime": 1612940513000,
          "title": "ECharts for React - 全网开发者下载量最高的 ECharts 的 React 组件封装",
          "order": 1,
          "hero": {
            "title": "ECharts for React",
            "desc": "<div class=\"markdown\"><p>全网开发者下载量最高的 ECharts 的 React 组件封装</p></div>",
            "actions": [
              {
                "text": "在线实例",
                "link": "/examples/dynamic"
              },
              {
                "text": "开源地址",
                "link": "https://github.com/hustcc/echarts-for-react"
              }
            ]
          },
          "footer": "<div class=\"markdown\"><p>Open-source MIT Licensed | Copyright © 2021-present</p></div>",
          "slugs": [
            {
              "depth": 2,
              "value": "安装",
              "heading": "安装"
            },
            {
              "depth": 2,
              "value": "使用",
              "heading": "使用"
            },
            {
              "depth": 2,
              "value": "反馈",
              "heading": "反馈"
            }
          ],
          "hasPreviewer": true
        },
        "title": "ECharts for React - 全网开发者下载量最高的 ECharts 的 React 组件封装 - \b"
      },
      {
        "path": "/examples/api",
        "component": require('/Users/xiaowei/Documents/code/antv/echarts-for-react/docs/examples/api.md').default,
        "exact": true,
        "meta": {
          "filePath": "docs/examples/api.md",
          "updatedTime": 1612692239000,
          "title": "ECharts API",
          "order": 2,
          "slugs": [
            {
              "depth": 2,
              "value": "ECharts API",
              "heading": "echarts-api"
            }
          ],
          "hasPreviewer": true,
          "nav": {
            "path": "/examples",
            "title": "Examples"
          }
        },
        "title": "ECharts API - \b"
      },
      {
        "path": "/examples/dynamic",
        "component": require('/Users/xiaowei/Documents/code/antv/echarts-for-react/docs/examples/dynamic.md').default,
        "exact": true,
        "meta": {
          "filePath": "docs/examples/dynamic.md",
          "updatedTime": 1754666951000,
          "title": "Dynamic",
          "order": 6,
          "slugs": [
            {
              "depth": 2,
              "value": "Dynamic",
              "heading": "dynamic"
            }
          ],
          "hasPreviewer": true,
          "nav": {
            "path": "/examples",
            "title": "Examples"
          }
        },
        "title": "Dynamic - \b"
      },
      {
        "path": "/examples/event",
        "component": require('/Users/xiaowei/Documents/code/antv/echarts-for-react/docs/examples/event.md').default,
        "exact": true,
        "meta": {
          "filePath": "docs/examples/event.md",
          "updatedTime": 1612692239000,
          "title": "Event",
          "order": 4,
          "slugs": [
            {
              "depth": 2,
              "value": "Event",
              "heading": "event"
            }
          ],
          "hasPreviewer": true,
          "nav": {
            "path": "/examples",
            "title": "Examples"
          }
        },
        "title": "Event - \b"
      },
      {
        "path": "/examples/gl",
        "component": require('/Users/xiaowei/Documents/code/antv/echarts-for-react/docs/examples/gl.md').default,
        "exact": true,
        "meta": {
          "filePath": "docs/examples/gl.md",
          "updatedTime": 1612692239000,
          "title": "Web GL",
          "order": 7,
          "slugs": [
            {
              "depth": 2,
              "value": "Web GL",
              "heading": "web-gl"
            }
          ],
          "hasPreviewer": true,
          "nav": {
            "path": "/examples",
            "title": "Examples"
          }
        },
        "title": "Web GL - \b"
      },
      {
        "path": "/examples/graph",
        "component": require('/Users/xiaowei/Documents/code/antv/echarts-for-react/docs/examples/graph.md').default,
        "exact": true,
        "meta": {
          "filePath": "docs/examples/graph.md",
          "updatedTime": 1612692239000,
          "title": "Graph",
          "order": 9,
          "slugs": [
            {
              "depth": 2,
              "value": "Graph",
              "heading": "graph"
            }
          ],
          "hasPreviewer": true,
          "nav": {
            "path": "/examples",
            "title": "Examples"
          }
        },
        "title": "Graph - \b"
      },
      {
        "path": "/examples/html-props",
        "component": require('/Users/xiaowei/Documents/code/antv/echarts-for-react/docs/examples/html-props.md').default,
        "exact": true,
        "meta": {
          "filePath": "docs/examples/html-props.md",
          "updatedTime": 1762393912000,
          "title": "HTML Properties",
          "order": 10,
          "slugs": [
            {
              "depth": 2,
              "value": "HTML Properties",
              "heading": "html-properties"
            }
          ],
          "hasPreviewer": true,
          "nav": {
            "path": "/examples",
            "title": "Examples"
          }
        },
        "title": "HTML Properties - \b"
      },
      {
        "path": "/examples/loading",
        "component": require('/Users/xiaowei/Documents/code/antv/echarts-for-react/docs/examples/loading.md').default,
        "exact": true,
        "meta": {
          "filePath": "docs/examples/loading.md",
          "updatedTime": 1612692239000,
          "title": "Loading",
          "order": 5,
          "slugs": [
            {
              "depth": 2,
              "value": "Loading",
              "heading": "loading"
            }
          ],
          "hasPreviewer": true,
          "nav": {
            "path": "/examples",
            "title": "Examples"
          }
        },
        "title": "Loading - \b"
      },
      {
        "path": "/examples/locale",
        "component": require('/Users/xiaowei/Documents/code/antv/echarts-for-react/docs/examples/locale.md').default,
        "exact": true,
        "meta": {
          "filePath": "docs/examples/locale.md",
          "updatedTime": 1614248129000,
          "title": "Locale",
          "order": 8,
          "slugs": [
            {
              "depth": 2,
              "value": "Locale",
              "heading": "locale"
            }
          ],
          "hasPreviewer": true,
          "nav": {
            "path": "/examples",
            "title": "Examples"
          }
        },
        "title": "Locale - \b"
      },
      {
        "path": "/examples/simple",
        "component": require('/Users/xiaowei/Documents/code/antv/echarts-for-react/docs/examples/simple.md').default,
        "exact": true,
        "meta": {
          "filePath": "docs/examples/simple.md",
          "updatedTime": 1754666951000,
          "title": "Simple",
          "order": 1,
          "slugs": [
            {
              "depth": 2,
              "value": "简单堆积面积图",
              "heading": "简单堆积面积图"
            }
          ],
          "hasPreviewer": true,
          "nav": {
            "path": "/examples",
            "title": "Examples"
          }
        },
        "title": "Simple - \b"
      },
      {
        "path": "/examples/svg",
        "component": require('/Users/xiaowei/Documents/code/antv/echarts-for-react/docs/examples/svg.md').default,
        "exact": true,
        "meta": {
          "filePath": "docs/examples/svg.md",
          "updatedTime": 1612692239000,
          "title": "SVG",
          "order": 8,
          "slugs": [
            {
              "depth": 2,
              "value": "SVG",
              "heading": "svg"
            }
          ],
          "hasPreviewer": true,
          "nav": {
            "path": "/examples",
            "title": "Examples"
          }
        },
        "title": "SVG - \b"
      },
      {
        "path": "/examples/theme",
        "component": require('/Users/xiaowei/Documents/code/antv/echarts-for-react/docs/examples/theme.md').default,
        "exact": true,
        "meta": {
          "filePath": "docs/examples/theme.md",
          "updatedTime": 1754666951000,
          "title": "Theme",
          "order": 3,
          "slugs": [
            {
              "depth": 2,
              "value": "Theme",
              "heading": "theme"
            }
          ],
          "hasPreviewer": true,
          "nav": {
            "path": "/examples",
            "title": "Examples"
          }
        },
        "title": "Theme - \b"
      },
      {
        "path": "/examples",
        "meta": {},
        "exact": true,
        "redirect": "/examples/simple"
      }
    ],
    "title": "\b",
    "component": (props) => props.children
  }
];

  // allow user to extend routes
  plugin.applyPlugins({
    key: 'patchRoutes',
    type: ApplyPluginsType.event,
    args: { routes },
  });

  return routes;
}
