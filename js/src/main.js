import Vue from 'vue'
import Router from 'vue-router'
import Resource from 'vue-resource'
import App from './App'

import TournamentList from './components/TournamentList.vue'

// install router
Vue.use(Router)
Vue.use(Resource)

// routing
var router = new Router({
  'hashbang': false,
  'history': true
})

router.map({
  '/towerfall/': {
    component: TournamentList
  }
})

router.beforeEach(function () {
  window.scrollTo(0, 0)
})

// As long as we only have Drunken TowerFall on drunkenfall.com, we should
// always redirect to the towerfall app right away.
router.redirect({
  '/': '/towerfall/'
})

router.start(App, 'app')
