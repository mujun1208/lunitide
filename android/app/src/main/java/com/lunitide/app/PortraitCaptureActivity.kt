// 竖屏扫码页：zxing-android-embedded 内置 CaptureActivity 在库清单里锁死
// android:screenOrientation="sensorLandscape"（已从本地 AAR 清单实锤），
// manifest merge 后 IntentIntegrator.setOrientationLocked(false) 完全无效——
// 0.17.6 实测「点扫码屏幕自动变横屏、扫描线竖着」正是它。
// 官方修复方案：空子类（全新组件，不与库声明产生 merge 冲突）+ 本应用
// 清单锁 portrait + setCaptureActivity 指向它。竖屏下取景框随屏幕宽向
// 横置、激光线水平扫动，符合「扫描线是横着的、不自动变横屏」标准。
package com.lunitide.app

import com.journeyapps.barcodescanner.CaptureActivity

class PortraitCaptureActivity : CaptureActivity()
