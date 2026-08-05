import { Link } from 'react-router-dom'
import { EmptyState } from '../components/ui/States'
import Button from '../components/ui/Button'

export default function NotFoundPage() {
  return (
    <EmptyState
      title="Page not found"
      description="That address does not lead anywhere in Pango."
      action={
        <Link to="/jobs">
          <Button variant="primary">Back to jobs</Button>
        </Link>
      }
    />
  )
}
