<script setup lang="ts">
const year = new Date().getFullYear()

const pains = [
  '“老板，那批货做到哪道工序了？”——要跑车间问一圈才答得上。',
  '“月底工资怎么算的？”——一沓菲票对到深夜，车工还觉得吃亏。',
  '“师傅一走，工序表也跟着走了。”——技术全在别人脑子里。',
  '“客户临时改单、催货期。”——嘴上答应了，心里没底。',
]

const features = [
  { icon: '▤', title: '订单与工单', desc: '款号、批量、货期、客户要求一单到底，改单留痕，进度实时可见。' },
  { icon: '◇', title: '工序与工价', desc: '工序表双人确认方为生产标准，历史款式一键做模板，增删改查即成新表。' },
  { icon: '◫', title: '进度与统计', desc: '车间、产线、个人产量层层汇总，货期风险提前亮灯，不再靠吼。' },
  { icon: '▣', title: '质检与库存', desc: '线检、终查、巡检全程填报，物料入库出库清清楚楚。' },
  { icon: '¥', title: '工资核算', desc: '扫菲计件、自动汇总，车工手机随时查自己的明白账。' },
  { icon: '⎙', title: '菲票与打印', desc: '扎号菲票一键打印，扫码报工，纸单数据两不误。' },
]

const roles = [
  { role: '厂长 / 经理', title: '全厂一盘棋', desc: '所有车间、产线、订单进度尽在掌握；客户与价格信息只对管理层可见，核心数据加密守护。' },
  { role: '车间主任', title: '车间一本账', desc: '本车间订单、人员、设备、工序进度一屏看清，排产调度有据可依。' },
  { role: '组长 / 线长', title: '产线一手抓', desc: '派工、质检、返工、出库一站办妥，与工艺员共同确认工序标准。' },
  { role: '车工', title: '收入自己查', desc: '干多少件、挣多少钱，手机扫码即知；干得明白，留得安心。' },
]

const steps = [
  { stage: '好奇', desc: '扫码或点链接进来，先看看别人怎么用。' },
  { stage: '感兴趣', desc: '免费体验开通，按导航亲手点一遍。' },
  { stage: '参与', desc: '把一个真实订单跑上线，感受顺畅。' },
  { stage: '促销', desc: '老厂推荐新厂，双方同享优惠。' },
  { stage: '成功', desc: '选定版本签约订阅，正式上云。' },
]

const plans = [
  { name: '微型厂版', size: '20 人以下', points: ['订单工单全流程', '扫菲计件与工资', '手机端全功能'], highlight: false },
  { name: '小型厂版', size: '50 人以下', points: ['含微型厂版全部', '多车间多产线', '质检库存全模块'], highlight: true },
  { name: '成长型厂版', size: '100 人以上', points: ['含小型厂版全部', '分车间订阅组合', '专属实施与培训'], highlight: false },
]

const roadmap = [
  { tag: '进行中', title: '产能评价体系', desc: '厂、车间、产线、人员四级产能与信誉评价，让好厂被更多订单看见。' },
  { tag: '规划中', title: 'AI 定制简介', desc: '基于评价数据，为小微厂自动生成可互动的接单简介页，帮厂揽活。' },
  { tag: '规划中', title: '语音录入', desc: '车间嘈杂腾不出手？说一声就完成报工。' },
  { tag: '规划中', title: 'AI 机器人服务', desc: '新手上路有问必答，操作引导随叫随到。' },
]
</script>

<template>
  <div class="portal">
    <!-- 顶部导航 -->
    <header class="p-nav">
      <div class="p-nav-inner">
        <div class="p-brand"><span class="p-brand-mark">智</span><span>一智算<small>服装生产协同云平台</small></span></div>
        <nav class="p-links">
          <a href="#pains">懂你的难</a>
          <a href="#features">产品能力</a>
          <a href="#roles">角色价值</a>
          <a href="#plans">价格方案</a>
          <a href="#roadmap">未来路线</a>
        </nav>
        <div class="p-nav-actions">
          <RouterLink to="/login" class="p-btn ghost">登 录</RouterLink>
          <a href="#join" class="p-btn solid">免费体验</a>
        </div>
      </div>
    </header>

    <!-- 主视觉 -->
    <section class="p-hero">
      <div class="p-hero-inner">
        <div class="p-badge">服装行业数字化协同平台 · 诚邀制衣厂入驻</div>
        <h1>让每一道工序都有数<br />让每一位车工都有账</h1>
        <p class="p-hero-sub">
          从接单到出货，从车间到工资单，一智算 把制衣厂的人和事搬到云上。
          手机、电脑同步协同，小厂用得起，大厂管得住。
        </p>
        <div class="p-hero-actions">
          <a href="#join" class="p-btn big solid">免费体验，即刻开局</a>
          <a href="#features" class="p-btn big outline">看看能做什么 ↓</a>
        </div>
        <div class="p-stats">
          <div class="p-stat"><strong>10+</strong><span>岗位角色全覆盖</span></div>
          <div class="p-stat"><strong>30+</strong><span>生产管理模块</span></div>
          <div class="p-stat"><strong>1 厂 1 密钥</strong><span>核心数据独立加密</span></div>
          <div class="p-stat"><strong>手机 + 电脑</strong><span>随时随地协同</span></div>
        </div>
      </div>
    </section>

    <!-- 痛点 -->
    <section id="pains" class="p-section p-pains">
      <h2>这些话，是不是很耳熟？</h2>
      <p class="p-section-sub">制衣厂的难，我们在车间里听过千百遍。</p>
      <div class="p-pain-grid">
        <blockquote v-for="(p, i) in pains" :key="i">{{ p }}</blockquote>
      </div>
      <p class="p-pain-foot">不是厂管不好，是工具没跟上。<strong>一智算 就是为此而来。</strong></p>
    </section>

    <!-- 产品能力 -->
    <section id="features" class="p-section">
      <h2>一个平台，管全厂</h2>
      <p class="p-section-sub">六大能力覆盖制衣生产全流程，开箱即用，随厂长大。</p>
      <div class="p-card-grid">
        <div v-for="f in features" :key="f.title" class="p-card">
          <div class="p-card-icon">{{ f.icon }}</div>
          <h3>{{ f.title }}</h3>
          <p>{{ f.desc }}</p>
        </div>
      </div>
    </section>

    <!-- 角色价值 -->
    <section id="roles" class="p-section p-roles">
      <h2>每个角色，都有自己的明白账</h2>
      <p class="p-section-sub">权限分得清，数据看得准，干活不扯皮。</p>
      <div class="p-card-grid four">
        <div v-for="r in roles" :key="r.role" class="p-card role">
          <div class="p-role-tag">{{ r.role }}</div>
          <h3>{{ r.title }}</h3>
          <p>{{ r.desc }}</p>
        </div>
      </div>
    </section>

    <!-- 安全 -->
    <section class="p-section p-security">
      <div class="p-security-inner">
        <h2>您的核心资产，我们当命根子守</h2>
        <p class="p-section-sub light">工序表是制衣厂的技术内核，客户与订单信息是生意的底牌。</p>
        <ul class="p-security-list">
          <li><strong>一厂一密钥</strong>客户、订单、单价、人员隐私，均按各厂独立密钥加密存储。</li>
          <li><strong>双人确认</strong>工序表须工艺员与线长共同确认方为生产标准，修改同样双确认。</li>
          <li><strong>分级授权</strong>谁能看哪张工序表，由厂长经理说了算，不能随手拷贝。</li>
          <li><strong>审计留痕</strong>关键操作全程记录，出了岔子查得清、追得回。</li>
        </ul>
      </div>
    </section>

    <!-- 入局流程 -->
    <section id="join" class="p-section">
      <h2>五步入局，零成本先试</h2>
      <p class="p-section-sub">先体验、再决定；老带新，双方都有礼。</p>
      <div class="p-steps">
        <div v-for="(s, i) in steps" :key="s.stage" class="p-step">
          <div class="p-step-no">{{ i + 1 }}</div>
          <h3>{{ s.stage }}</h3>
          <p>{{ s.desc }}</p>
        </div>
      </div>
    </section>

    <!-- 价格方案 -->
    <section id="plans" class="p-section p-plans">
      <h2>按规模选版本，按地区定价格</h2>
      <p class="p-section-sub">订阅制付费，用多少人买多少量；不同国家地区，币种价格各自相宜。一个厂还可为不同车间订阅不同版本。</p>
      <div class="p-card-grid three">
        <div v-for="p in plans" :key="p.name" class="p-card plan" :class="{ highlight: p.highlight }">
          <div class="p-plan-size">{{ p.size }}</div>
          <h3>{{ p.name }}</h3>
          <ul>
            <li v-for="pt in p.points" :key="pt">{{ pt }}</li>
          </ul>
          <a href="#join" class="p-btn" :class="p.highlight ? 'solid' : 'ghost'">咨询订阅</a>
        </div>
      </div>
    </section>

    <!-- 未来路线 -->
    <section id="roadmap" class="p-section">
      <h2>开拓新土，共创未来</h2>
      <p class="p-section-sub">入驻不是终点，而是一起长大的起点。这些能力正在路上，早来的厂先受益。</p>
      <div class="p-card-grid four">
        <div v-for="r in roadmap" :key="r.title" class="p-card roadmap">
          <span class="p-roadmap-tag">{{ r.tag }}</span>
          <h3>{{ r.title }}</h3>
          <p>{{ r.desc }}</p>
        </div>
      </div>
    </section>

    <!-- 底部 CTA -->
    <section class="p-final">
      <h2>今天多一个数，明天少跑十趟车间</h2>
      <p>免费体验现已开放，诚邀制衣厂同行入局，一起把制衣这件事做得更明白。</p>
      <div class="p-hero-actions">
        <a href="#join" class="p-btn big solid">免费体验</a>
        <RouterLink to="/login" class="p-btn big outline">已有账号，直接登录</RouterLink>
      </div>
    </section>

    <footer class="p-footer">
      <div class="p-footer-grid">
        <div class="p-footer-col brand-col">
          <div class="p-brand small"><span class="p-brand-mark">智</span><span>一智算 服装生产协同云平台</span></div>
          <p class="p-footer-slogan">引商入局 · 开拓新土 · 共创未来</p>
          <p class="p-footer-desc">专注制衣行业数字化，让每一道工序都有数，让每一位车工都有账。</p>
        </div>
        <div class="p-footer-col">
          <h4>联系我们</h4>
          <p>商务合作：0771-XXXXXXX</p>
          <p>微信咨询：yzs-service（一智算客服）</p>
          <p>邮箱：business@yizhisuan.cn</p>
        </div>
        <div class="p-footer-col">
          <h4>公司地址</h4>
          <p>广西南宁市青秀区<br />XX路 XX 号 XX 大厦 XX 层</p>
          <p class="p-footer-note">（正式地址以营业执照登记为准）</p>
        </div>
        <div class="p-footer-col qr-col">
          <h4>免费体验</h4>
          <div class="p-qr">二维码<br />占位</div>
          <p class="p-footer-note">扫码体验 / 添加客服</p>
        </div>
      </div>
      <div class="p-footer-bar">
        <p>© {{ year }} 南宁一智万商科技有限责任公司 版权所有</p>
        <p><a href="https://beian.miit.gov.cn" target="_blank" rel="noopener">桂ICP备XXXXXXXX号-X</a><span class="p-footer-sep">|</span><span>桂公网安备 XXXXXXXXXXXXX号</span></p>
      </div>
    </footer>
  </div>
</template>

<style scoped>
.portal{min-height:100vh;background:#fff;color:#172033;font-family:Inter,"PingFang SC","Microsoft YaHei",sans-serif}
/* 导航 */
.p-nav{position:sticky;top:0;z-index:50;background:rgba(255,255,255,.92);backdrop-filter:blur(10px);border-bottom:1px solid #eef1f6}
.p-nav-inner{max-width:1180px;margin:0 auto;height:64px;display:flex;align-items:center;gap:28px;padding:0 24px}
.p-brand{display:flex;align-items:center;gap:10px;font-weight:800;font-size:18px;letter-spacing:-.01em}
.p-brand small{display:block;font-size:11px;font-weight:500;color:#7a8699}
.p-brand-mark{display:grid;place-items:center;width:34px;height:34px;border-radius:9px;background:#155eef;color:#fff;font-size:13px;flex-shrink:0}
.p-links{display:flex;gap:22px;margin-left:auto}
.p-links a{font-size:14px;color:#475467}.p-links a:hover{color:#155eef}
.p-nav-actions{display:flex;gap:10px}
/* 按钮 */
.p-btn{display:inline-flex;align-items:center;justify-content:center;border-radius:8px;padding:9px 18px;font-size:14px;font-weight:600;border:1px solid transparent;transition:.15s}
.p-btn.solid{background:#155eef;color:#fff}.p-btn.solid:hover{background:#0e4fd6}
.p-btn.ghost{border-color:#d0d5dd;color:#344054;background:#fff}.p-btn.ghost:hover{border-color:#155eef;color:#155eef}
.p-btn.outline{border-color:rgba(255,255,255,.5);color:#fff}.p-btn.outline:hover{background:rgba(255,255,255,.12)}
.p-btn.big{padding:14px 30px;font-size:16px;border-radius:10px}
/* 主视觉 */
.p-hero{background:radial-gradient(1100px 500px at 75% -10%,#2f6df0 0%,transparent 60%),linear-gradient(160deg,#0c1e42 0%,#123064 55%,#155eef 130%);color:#fff;position:relative;overflow:hidden}
.p-hero::after{content:"";position:absolute;inset:0;background-image:linear-gradient(rgba(255,255,255,.05) 1px,transparent 1px),linear-gradient(90deg,rgba(255,255,255,.05) 1px,transparent 1px);background-size:44px 44px;mask-image:linear-gradient(to bottom,#000 30%,transparent 90%)}
.p-hero-inner{position:relative;z-index:1;max-width:1180px;margin:0 auto;padding:96px 24px 88px;text-align:center}
.p-badge{display:inline-block;border:1px solid rgba(255,255,255,.35);border-radius:999px;padding:6px 16px;font-size:13px;color:#cfe0ff;margin-bottom:26px}
.p-hero h1{font-size:52px;line-height:1.25;margin:0 0 20px;letter-spacing:-.02em}
.p-hero-sub{max-width:680px;margin:0 auto 36px;color:#c9d8f5;font-size:17px;line-height:1.9}
.p-hero-actions{display:flex;gap:14px;justify-content:center;flex-wrap:wrap}
.p-stats{display:grid;grid-template-columns:repeat(4,1fr);gap:16px;max-width:860px;margin:64px auto 0}
.p-stat{border:1px solid rgba(255,255,255,.18);border-radius:12px;padding:18px 10px;background:rgba(255,255,255,.06)}
.p-stat strong{display:block;font-size:22px;margin-bottom:6px}
.p-stat span{font-size:13px;color:#b9cbf0}
/* 区块通用 */
.p-section{max-width:1180px;margin:0 auto;padding:84px 24px 20px}
.p-section h2{font-size:34px;text-align:center;margin:0 0 12px;letter-spacing:-.02em}
.p-section-sub{text-align:center;color:#667085;font-size:16px;margin:0 auto 44px;max-width:720px;line-height:1.8}
.p-section-sub.light{color:#b9cbf0}
/* 痛点 */
.p-pains h2{color:#172033}
.p-pain-grid{display:grid;grid-template-columns:1fr 1fr;gap:16px;max-width:900px;margin:0 auto}
.p-pain-grid blockquote{margin:0;background:#f6f8fc;border-left:4px solid #f79009;border-radius:0 10px 10px 0;padding:18px 20px;font-size:15px;line-height:1.8;color:#344054}
.p-pain-foot{text-align:center;margin-top:36px;font-size:17px;color:#475467}
.p-pain-foot strong{color:#155eef}
/* 卡片 */
.p-card-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:18px}
.p-card-grid.four{grid-template-columns:repeat(4,1fr)}
.p-card-grid.three{grid-template-columns:repeat(3,1fr)}
.p-card{border:1px solid #e6eaf2;border-radius:14px;padding:26px 24px;background:#fff;transition:.2s}
.p-card:hover{transform:translateY(-4px);box-shadow:0 14px 34px rgba(16,24,40,.09);border-color:#c9d9fb}
.p-card-icon{width:44px;height:44px;border-radius:11px;background:#eff4ff;color:#155eef;display:grid;place-items:center;font-size:21px;margin-bottom:16px}
.p-card h3{font-size:18px;margin:0 0 10px}
.p-card p{margin:0;color:#667085;font-size:14px;line-height:1.8}
.p-card.role{background:linear-gradient(180deg,#f8faff,#fff)}
.p-role-tag{display:inline-block;background:#155eef;color:#fff;border-radius:6px;font-size:12px;padding:4px 10px;margin-bottom:14px}
/* 安全 */
.p-security{max-width:none;background:linear-gradient(150deg,#0c1e42,#163a7a);color:#fff;margin-top:64px;padding-top:76px;padding-bottom:76px}
.p-security-inner{max-width:900px;margin:0 auto}
.p-security h2{color:#fff}
.p-security-list{list-style:none;margin:0;padding:0;display:grid;grid-template-columns:1fr 1fr;gap:14px}
.p-security-list li{background:rgba(255,255,255,.07);border:1px solid rgba(255,255,255,.16);border-radius:12px;padding:18px 20px;font-size:14px;line-height:1.8;color:#d5e2f8}
.p-security-list strong{display:block;color:#fff;font-size:15px;margin-bottom:6px}
/* 五步流程 */
.p-steps{display:grid;grid-template-columns:repeat(5,1fr);gap:14px}
.p-step{position:relative;border:1px solid #e6eaf2;border-radius:14px;padding:24px 18px;background:#fff}
.p-step-no{width:34px;height:34px;border-radius:50%;background:#eff4ff;color:#155eef;font-weight:700;display:grid;place-items:center;margin-bottom:14px}
.p-step h3{margin:0 0 8px;font-size:17px}
.p-step p{margin:0;color:#667085;font-size:13px;line-height:1.7}
/* 价格 */
.p-card.plan{display:flex;flex-direction:column}
.p-card.plan.highlight{border-color:#155eef;box-shadow:0 10px 30px rgba(21,94,239,.14);position:relative}
.p-card.plan.highlight::before{content:"热门选择";position:absolute;top:-12px;right:20px;background:#155eef;color:#fff;font-size:12px;border-radius:999px;padding:3px 12px}
.p-plan-size{color:#155eef;font-weight:700;font-size:14px;margin-bottom:8px}
.p-card.plan ul{list-style:none;padding:0;margin:6px 0 22px;flex:1}
.p-card.plan li{padding:8px 0 8px 24px;position:relative;color:#475467;font-size:14px;border-bottom:1px dashed #edf0f5}
.p-card.plan li::before{content:"✓";position:absolute;left:2px;color:#039855;font-weight:700}
/* 路线图 */
.p-card.roadmap .p-roadmap-tag{display:inline-block;border-radius:999px;padding:3px 12px;font-size:12px;background:#ecfdf3;color:#027a48;margin-bottom:12px}
/* 收尾 */
.p-final{text-align:center;padding:96px 24px;background:radial-gradient(800px 340px at 50% 120%,#2f6df0 0%,transparent 70%),linear-gradient(160deg,#0c1e42,#123a7e);color:#fff}
.p-final h2{font-size:36px;margin:0 0 14px;letter-spacing:-.02em}
.p-final p{color:#c9d8f5;font-size:16px;margin:0 0 34px}
.p-footer{background:#0a1830;color:#8fa3c8;padding:56px 24px 0;font-size:13px}
.p-footer-grid{max-width:1180px;margin:0 auto;display:grid;grid-template-columns:1.6fr 1fr 1fr .8fr;gap:36px;padding-bottom:40px}
.p-footer-col h4{color:#fff;font-size:14px;margin:0 0 14px}
.p-footer-col p{margin:0 0 8px;line-height:1.7}
.p-footer .p-brand{color:#fff;font-size:15px}
.p-footer-slogan{margin:14px 0 6px;color:#c9d8f5;font-size:14px;letter-spacing:.1em}
.p-footer-desc{line-height:1.8}
.p-footer-note{color:#5f7398;font-size:12px}
.p-qr{width:92px;height:92px;border:1px dashed #415980;border-radius:10px;display:grid;place-items:center;text-align:center;color:#5f7398;font-size:12px;line-height:1.6;margin-bottom:10px}
.p-footer-bar{max-width:1180px;margin:0 auto;border-top:1px solid #1c2c48;padding:18px 0 22px;display:flex;flex-wrap:wrap;gap:8px 24px;justify-content:space-between}
.p-footer-bar p{margin:0}
.p-footer-bar a{color:#8fa3c8}.p-footer-bar a:hover{color:#fff}
.p-footer-sep{margin:0 12px;color:#31456a}
@media(max-width:960px){
  .p-links{display:none}
  .p-hero h1{font-size:36px}
  .p-hero-inner{padding:64px 20px 60px}
  .p-stats{grid-template-columns:1fr 1fr;gap:10px;margin-top:44px}
  .p-card-grid,.p-card-grid.four,.p-card-grid.three{grid-template-columns:1fr 1fr}
  .p-pain-grid{grid-template-columns:1fr}
  .p-security-list{grid-template-columns:1fr}
  .p-steps{grid-template-columns:1fr 1fr}
  .p-section{padding:60px 18px 10px}
  .p-section h2,.p-final h2{font-size:26px}
  .p-footer-grid{grid-template-columns:1fr 1fr;gap:26px}
}
@media(max-width:600px){
  .p-card-grid,.p-card-grid.four,.p-card-grid.three,.p-steps{grid-template-columns:1fr}
  .p-nav-actions .ghost{display:none}
  .p-hero h1{font-size:29px}
  .p-footer-grid{grid-template-columns:1fr;gap:22px}
}
</style>
