// 修补 Arco Design 在 React 19 下的兼容问题：
// 1) 读取 element.ref 触发 "Accessing element.ref was removed in React 19"
// 2) react-dom 不再导出 createRoot，Arco 回退到已移除的 ReactDOM.render，
//    导致 "CopyReactDOM.render is not a function"
import fs from "node:fs";
import path from "node:path";

const ES = "node_modules/@arco-design/web-react/es";
const LIB = "node_modules/@arco-design/web-react/lib";

function patch(file, from, to) {
  const p = path.resolve(process.cwd(), file);
  if (!fs.existsSync(p)) return 0;
  const s = fs.readFileSync(p, "utf8");
  if (s.includes(to)) return 0;
  if (!s.includes(from)) {
    console.warn(`[patch-arco] 未匹配：${file}`);
    return 0;
  }
  fs.writeFileSync(p, s.replace(from, to));
  return 1;
}

function insertAfter(file, marker, insert) {
  const p = path.resolve(process.cwd(), file);
  if (!fs.existsSync(p)) return 0;
  const s = fs.readFileSync(p, "utf8");
  if (s.includes(insert)) return 0;
  if (!s.includes(marker)) {
    console.warn(`[patch-arco] 未匹配 marker：${file}`);
    return 0;
  }
  fs.writeFileSync(p, s.replace(marker, marker + insert));
  return 1;
}

let n = 0;

// 1. callbackOriginRef
n += patch(
  `${ES}/_util/react-dom.js`,
  `export var callbackOriginRef = function (children, node) {
    if (children && children.ref) {
        if (isFunction(children.ref)) {
            children === null || children === void 0 ? void 0 : children.ref(node);
        }
        if ('current' in children.ref) {
            children.ref.current = node;
        }
    }
};`,
  `export var callbackOriginRef = function (children, node) {
    var ref = children && children.props && children.props.ref;
    if (ref) {
        if (isFunction(ref)) {
            ref(node);
        } else if ('current' in ref) {
            ref.current = node;
        }
    }
};`,
);
n += patch(
  `${LIB}/_util/react-dom.js`,
  `var callbackOriginRef = function (children, node) {
    if (children && children.ref) {
        if ((0, is_1.isFunction)(children.ref)) {
            children === null || children === void 0 ? void 0 : children.ref(node);
        }
        if ('current' in children.ref) {
            children.ref.current = node;
        }
    }
};`,
  `var callbackOriginRef = function (children, node) {
    var ref = children && children.props && children.props.ref;
    if (ref) {
        if ((0, is_1.isFunction)(ref)) {
            ref(node);
        } else if ('current' in ref) {
            ref.current = node;
        }
    }
};`,
);

// 2. Trigger / AutoComplete 读取 .ref
for (const base of [ES, LIB]) {
  n += patch(`${base}/Trigger/index.js`, "{ ref: popupChildren.ref }", "{ ref: popupChildren.props && popupChildren.props.ref }");
  n += patch(
    `${base}/AutoComplete/index.js`,
    "var originRef = usedTriggerElement.ref;",
    "var originRef = usedTriggerElement.props && usedTriggerElement.props.ref;",
  );
}

// 3. createRoot 回退（React 19 的 react-dom 不导出 createRoot）
n += insertAfter(
  `${ES}/_util/react-dom.js`,
  "import { Component } from 'react';",
  "\nimport { createRoot as __arcoCreateRoot } from 'react-dom/client';",
);
n += insertAfter(
  `${LIB}/_util/react-dom.js`,
  'var react_dom_1 = __importDefault(require("react-dom"));',
  '\nvar react_dom_client_1 = require("react-dom/client");',
);
n += patch(
  `${ES}/_util/react-dom.js`,
  `var createRoot;
try {
    createRoot = CopyReactDOM.createRoot;
}`,
  `var createRoot;
try {
    createRoot = CopyReactDOM.createRoot || __arcoCreateRoot;
}`,
);
n += patch(
  `${LIB}/_util/react-dom.js`,
  `var createRoot;
try {
    createRoot = CopyReactDOM.createRoot;
}`,
  `var createRoot;
try {
    createRoot = CopyReactDOM.createRoot || react_dom_client_1.createRoot;
}`,
);

console.log(n > 0 ? `[patch-arco] 已修补 ${n} 处` : "[patch-arco] 无需修补");
