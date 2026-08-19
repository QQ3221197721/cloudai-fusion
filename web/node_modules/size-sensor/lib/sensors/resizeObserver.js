"use strict";

Object.defineProperty(exports, "__esModule", {
  value: true
});
exports.createSensor = void 0;
var _constant = require("../constant");
var _debounce = _interopRequireDefault(require("../debounce"));
function _interopRequireDefault(e) { return e && e.__esModule ? e : { "default": e }; }
/**
 * Created by hustcc on 18/7/5.
 * Contract: i@hust.cc
 */

var createSensor = exports.createSensor = function createSensor(element, whenDestroy) {
  var sensor = undefined;
  // callback
  var listeners = [];

  /**
   * trigger listeners
   */
  var resizeListener = (0, _debounce["default"])(function () {
    // trigger all
    listeners.forEach(function (listener) {
      listener(element);
    });
  });

  /**
   * create ResizeObserver sensor
   * @returns
   */
  var newSensor = function newSensor() {
    var s = new ResizeObserver(resizeListener);
    // listen element
    s.observe(element);

    // trigger once
    resizeListener();
    return s;
  };

  /**
   * listen with callback
   * @param cb
   */
  var bind = function bind(cb) {
    if (!sensor) {
      sensor = newSensor();
    }
    if (listeners.indexOf(cb) === -1) {
      listeners.push(cb);
    }
  };

  /**
   * destroy
   */
  var destroy = function destroy() {
    if (sensor) {
      sensor.disconnect();
    }
    listeners = [];
    sensor = undefined;
    element.removeAttribute(_constant.SizeSensorId);
    whenDestroy && whenDestroy();
  };

  /**
   * cancel bind
   * @param cb
   */
  var unbind = function unbind(cb) {
    var idx = listeners.indexOf(cb);
    if (idx !== -1) {
      listeners.splice(idx, 1);
    }

    // no listener, and sensor is exist
    // then destroy the sensor
    if (listeners.length === 0 && sensor) {
      destroy();
    }
  };
  return {
    element: element,
    bind: bind,
    destroy: destroy,
    unbind: unbind
  };
};