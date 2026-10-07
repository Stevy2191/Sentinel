import { Link } from 'react-router-dom'

/** Shown instead of the tools to anyone who is neither an admin nor granted
 *  network tools; the API would refuse them anyway. */
export default function NoToolsAccess() {
  return (
    <div className="card p-8 text-center">
      <p className="text-slate-300">Network tools are for admins and for users an admin has given access.</p>
      <Link to="/" className="mt-2 inline-block text-sm text-primary-400 hover:underline">
        Back to the overview
      </Link>
    </div>
  )
}
