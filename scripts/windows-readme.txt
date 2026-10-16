DocAI Gateway - 文档 AI 智能网关
====================================

快速开始:
  1. 双击 doc-ai-gateway.exe 启动服务
  2. 打开浏览器访问 http://localhost:8080
  3. 使用默认账号 admin/admin 登录

注意:
  admin 首次登录必须修改密码，2小时内未修改则初始密码作废
  程序运行日志见 logs/ 目录，按小时滚动，保留7天

目录说明:
  doc-ai-gateway.exe  主程序
  cfg.yml             配置文件
  data\                文件存储目录 (自动创建)

对外接口:
  POST /api/files/upload       上传文件
  GET  /api/files/status/:id   查询文件状态
  POST /api/ai/call            调用 AI 接口

管理后台: http://localhost:8080/admin
