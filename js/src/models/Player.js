import store from '../core/store'
// import Person from './Person.js'
import PlayerState from './PlayerState'

export default class Player {
  static fromObject (obj) {
    let p = new Player()
    Object.assign(p, obj)
    // p.person = Person.fromObject(p.person)
    // p.state = PlayerState.fromObject(p.state)
    return p
  }

  // TODO(thiderman): Fix so that every avatar is set per tournament
  get avatar () {
    if (this.person.avatar_url) {
      return this.person.avatar_url
    }
    return "https://graph.facebook.com/" + this.person.facebook_id + "/picture?width=9999"
  }

  get displayName () {
    return this.person.nick
  }

  get firstName () {
    return this.person.name.split(" ")[0]
  }

  get state () {
    let s = store.getters.getPlayerState(this.index)
    return PlayerState.fromObject(s)
  }

  set state (x) {
    return
  }

  get person () {
    return store.getters.getPerson(this.person_id)
  }

  set person (x) {
    return
  }

}
