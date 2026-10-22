doc_flow_agent - 文档 AI 智能网关
====================================

快速开始:
  1. 双击 doc_flow_agent.exe 启动服务
  2. 打开浏览器访问 http://localhost:8080/admin
  3. 使用默认账号登录:
       用户名: admin
       密码:   admin

注意:
  admin 首次登录必须修改密码，2小时内未修改则初始密码作废
  程序运行日志见 data\logs\ 目录，按小时滚动，保留7天

目录说明:
  doc_flow_agent.exe  主程序
  cfg.yml             配置文件
  data\                数据目录（数据库、日志、上传文件）

对外接口:
  POST /api/files/upload       上传文件
  GET  /api/files/status/:id   查询文件状态
  POST /api/ai/call            调用 AI 接口

管理后台: http://localhost:8080/admin
